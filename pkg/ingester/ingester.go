package ingester

import (
	"context"
	"errors"
	"fmt"
	"io"

	"tensorvault/pkg/chunker"
	"tensorvault/pkg/core"
	"tensorvault/pkg/storage"

	"golang.org/x/sync/errgroup"
)

// 配置常量
const (
	WorkerCount    = 16              // 并发上传的 Worker 数量
	ReadBufferSize = 1 * 1024 * 1024 // 每次从磁盘读取 1MB 进行处理 (Generator buffer)
)

type Ingester struct {
	store   storage.Store
	chunker *chunker.Chunker
}

func NewIngester(store storage.Store) *Ingester {
	return &Ingester{
		store:   store,
		chunker: chunker.NewChunker(),
	}
}

// job 代表一个待处理的 Chunk 任务 (Generator -> Worker)
type job struct {
	index int    // 顺序号，用于最后重组
	data  []byte // 原始数据
}

// result 代表一个处理完的 Chunk 结果 (Worker -> Collector)
type result struct {
	index int
	link  core.ChunkLink
}

// IngestFile 使用流水线并发处理文件
func (ing *Ingester) IngestFile(ctx context.Context, reader io.Reader) (*core.FileNode, error) {

	// channels 带有 buffer，起到背压 (Backpressure) 的作用
	jobsCh := make(chan job, WorkerCount*2)
	resultsCh := make(chan result, WorkerCount*2)
	// 1. 创建专门管理生产者的 ErrGroup
	producerG, producerCtx := errgroup.WithContext(ctx)

	// 2. 启动 Generator (属于 Layer A)
	producerG.Go(func() error {
		defer close(jobsCh)
		return ing.generateJobs(producerCtx, reader, jobsCh)
	})

	// 3. 启动 Workers (属于 Layer A)
	for range WorkerCount {
		producerG.Go(func() error {
			for j := range jobsCh {
				chunkObj := core.NewChunk(j.data)
				// 注意：这里用的是 producerCtx，一旦报错，所有 Worker + Gen 立即停止
				if err := ing.store.Put(producerCtx, chunkObj); err != nil {
					return err
				}
				select {
				case resultsCh <- result{index: j.index, link: core.NewChunkLink(chunkObj)}:
				case <-producerCtx.Done():
					return producerCtx.Err()
				}
			}
			return nil
		})
	}

	// 守护协程：等待生产者结束 -> 关闭结果通道(Layer B)
	go func() {
		_ = producerG.Wait() //nolint:errcheck // 错误会由 collector 最终捕获或传播
		close(resultsCh)
	}()

	// 主线程：执行 Collector(Layer C)
	// 如果 producerG 出错，resultsCh 会被关闭（因为 generateJobs 或 worker 退出），
	// 或者 ctx 被 cancel。Collector 会在读取 channel 或 ctx check 时感知到。
	fileNode, err := ing.collect(ctx, resultsCh)
	if err != nil {
		// 优先返回 producer 的错误（那是根因）
		if pErr := producerG.Wait(); pErr != nil {
			return nil, pErr
		}
		return nil, err
	}

	// 再次检查 producer 错误确保万无一失
	if err := producerG.Wait(); err != nil {
		return nil, fmt.Errorf("pipeline execution failed: %w", err)
	}

	// 最后：存储生成的 FileNode
	if err := ing.store.Put(ctx, fileNode); err != nil {
		return nil, fmt.Errorf("failed to store filenode: %w", err)
	}

	return fileNode, nil
}

// generateJobs 实现流式 CDC 切分
func (ing *Ingester) generateJobs(ctx context.Context, reader io.Reader, jobsCh chan<- job) error {
	buffer := make([]byte, ReadBufferSize)
	var remainder []byte // 暂存上一次切分剩下的“尾巴”
	chunkIndex := 0

	for {
		// 1. 读取数据块
		n, err := reader.Read(buffer)
		if n > 0 {
			// 拼接：余数 + 新数据
			// 必须 make new slice，因为 buffer 会在下一次循环被覆盖
			processingData := make([]byte, len(remainder)+n)
			copy(processingData, remainder)
			copy(processingData[len(remainder):], buffer[:n])

			// 2. 执行 CDC 切分
			cutPoints := ing.chunker.Cut(processingData)
			start := 0

			for _, end := range cutPoints {

				chunkData := make([]byte, end-start)
				copy(chunkData, processingData[start:end])

				select {
				case jobsCh <- job{index: chunkIndex, data: chunkData}:
				case <-ctx.Done():
					return ctx.Err()
				}
				chunkIndex++
				start = end
			}

			// 3. 更新 Remainder
			// 剩下的数据就是 processingData 从最后一个 start 开始的部分
			if start < len(processingData) {
				remainder = make([]byte, len(processingData)-start)
				copy(remainder, processingData[start:])
			} else {
				remainder = nil
			}
		}
		if errors.Is(err, io.EOF) {
			// EOF 时，如果还有 remainder，说明它是最后一个块
			if len(remainder) > 0 {
				select {
				case jobsCh <- job{index: chunkIndex, data: remainder}:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return nil
		}
		if err != nil {
			return fmt.Errorf("read error: %w", err)
		}
	}
}

// collect 收集结果并重组
func (ing *Ingester) collect(_ context.Context, results <-chan result) (*core.FileNode, error) {
	// 乱序缓冲池
	pending := make(map[int]core.ChunkLink)

	// 有序列表
	var chunks []core.ChunkLink
	nextIndex := 0 // 我们当前期待的 index
	var totalSize int64

	for res := range results {
		// 1. 存入缓冲
		pending[res.index] = res.link

		// 2. 尝试提取连续的块
		// 只要 pending[nextIndex] 存在，就说明下一个块到了，可以取出来
		for {
			link, ok := pending[nextIndex]
			if !ok {
				break // 缺货，继续等
			}

			// 组装
			chunks = append(chunks, link)
			totalSize += int64(link.Size)

			// 清理并推进游标
			delete(pending, nextIndex)
			nextIndex++
		}
	}

	// 循环结束时，如果 pending 还有剩余，说明中间有块丢失了
	if len(pending) > 0 {
		return nil, fmt.Errorf("integrity error: missing chunks in sequence (pending: %d)", len(pending))
	}

	return core.NewFileNode(totalSize, chunks)
}
