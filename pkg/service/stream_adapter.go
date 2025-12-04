package service

import (
	"fmt"

	tvrpc "tensorvault/pkg/api/tvrpc/v1"
)

// =============================================================================
// 1. Upload Adapter: gRPC Stream -> io.Reader
// =============================================================================

// UploadStream 定义了 Upload 接口所需的最小集合，方便测试 Mock
type UploadStream interface {
	Recv() (*tvrpc.UploadRequest, error)
}

// GrpcStreamReader 将 gRPC Upload 流包装为 io.Reader
// 供 pkg/ingester 使用
type GrpcStreamReader struct {
	stream      UploadStream
	internalBuf []byte // 内部缓冲：存储从 Recv 拿到的、还没被 Read 读走的数据
	err         error  // 存储流的状态错误 (如 EOF)
}

func NewGrpcStreamReader(stream UploadStream) *GrpcStreamReader {
	return &GrpcStreamReader{
		stream: stream,
	}
}

// Read 实现了 io.Reader 接口
// 这是一个典型的“缓冲-消费”状态机
func (r *GrpcStreamReader) Read(p []byte) (n int, err error) {
	// 如果已经有错误（比如 EOF），直接返回
	if r.err != nil {
		return 0, r.err
	}

	// 如果内部缓冲为空，需要去 gRPC 拉取新数据
	if len(r.internalBuf) == 0 {
		req, err := r.stream.Recv()
		if err != nil {
			r.err = err // 记住错误 (可能是 io.EOF)
			return 0, err
		}

		// 处理 oneof：我们只关心二进制数据
		// 注意：Meta 数据应该在 Service 层最开始就处理掉了，这里只应该收到 ChunkData
		switch payload := req.Payload.(type) {
		case *tvrpc.UploadRequest_ChunkData:
			r.internalBuf = payload.ChunkData
		case *tvrpc.UploadRequest_Meta:
			// 理论上不应该在流中间收到 Meta，但如果收到了，我们可以忽略或报错
			// 为了健壮性，这里选择跳过本次循环，递归调用自己去读下一帧
			return r.Read(p)
		default:
			// 空包或未知类型，继续读
			return r.Read(p)
		}
	}

	// 此时 internalBuf 一定有数据
	// 能够复制多少字节？取决于 p 的容量和 internalBuf 的长度
	copied := copy(p, r.internalBuf)

	// 消费掉缓冲区已读的部分
	r.internalBuf = r.internalBuf[copied:]

	return copied, nil
}

// =============================================================================
// 2. Download Adapter: io.Writer -> gRPC Stream
// =============================================================================

// DownloadStream 定义了 Download 接口所需的最小集合
type DownloadStream interface {
	Send(*tvrpc.DownloadResponse) error
}

// GrpcStreamWriter 将 gRPC Download 流包装为 io.Writer
// 供 pkg/exporter 使用
type GrpcStreamWriter struct {
	stream DownloadStream
}

func NewGrpcStreamWriter(stream DownloadStream) *GrpcStreamWriter {
	return &GrpcStreamWriter{stream: stream}
}

// Write 实现了 io.Writer 接口
// Exporter 每写一块数据，这里就发一个 gRPC 包
func (w *GrpcStreamWriter) Write(p []byte) (n int, err error) {
	// 防御性拷贝：虽然 gRPC Send 会立刻序列化，但为了安全起见
	// 如果 p 在外部被复用，直接引用可能会导致竞态。
	// 不过在 Exporter 中我们是读文件流，一般是安全的。
	// 这里为了性能，我们直接发送。如果有问题再加 copy。

	resp := &tvrpc.DownloadResponse{
		ChunkData: p,
	}

	if err := w.stream.Send(resp); err != nil {
		return 0, fmt.Errorf("grpc send failed: %w", err)
	}

	return len(p), nil
}

// io.WriterAt 接口适配 (可选)
// 我们的 Exporter 对于不支持 WriterAt 的 writer 会回退到串行模式
// gRPC 流本质上是串行的，所以我们不需要实现 WriteAt。
// Exporter 会自动检测到这一点，并使用串行逻辑，这正是我们要的。
