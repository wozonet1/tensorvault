package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"testing"

	tvrpc "tensorvault/pkg/api/tvrpc/v1"
	"tensorvault/pkg/app"
	"tensorvault/pkg/core"
	"tensorvault/pkg/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// =============================================================================
// Mocks (模拟 gRPC 流的行为)
// =============================================================================

// MockUploadStream 模拟客户端流式发送
type MockUploadStream struct {
	grpc.ServerStream // 嵌入以满足接口，虽然我们只覆盖 Recv
	Ctx               context.Context
	Requests          []*tvrpc.UploadRequest // 预设的输入队列
	cursor            int                    // 当前读到哪里
	Response          *tvrpc.UploadResponse  // 捕获最终响应
}

func (m *MockUploadStream) Context() context.Context {
	if m.Ctx == nil {
		return context.Background()
	}
	return m.Ctx
}

func (m *MockUploadStream) Recv() (*tvrpc.UploadRequest, error) {
	if m.cursor >= len(m.Requests) {
		return nil, io.EOF
	}
	req := m.Requests[m.cursor]
	m.cursor++
	return req, nil
}

func (m *MockUploadStream) SendAndClose(resp *tvrpc.UploadResponse) error {
	m.Response = resp
	return nil
}

// MockDownloadStream 模拟服务端流式响应
type MockDownloadStream struct {
	grpc.ServerStream
	Ctx       context.Context
	Responses []*tvrpc.DownloadResponse // 捕获服务端发来的数据
}

func (m *MockDownloadStream) Context() context.Context {
	if m.Ctx == nil {
		return context.Background()
	}
	return m.Ctx
}

func (m *MockDownloadStream) Send(resp *tvrpc.DownloadResponse) error {
	m.Responses = append(m.Responses, resp)
	return nil
}

func setupTestDataService(t *testing.T) (*DataService, *app.App) {
	// 复用 meta_test.go 里的逻辑太麻烦，直接重写一个更干净的
	// 或者把 app 初始化逻辑提取到 helpers_test.go

	app := setupTestApp(t)

	return NewDataService(app), app
}

// =============================================================================
// Tests
// =============================================================================

func TestDataService_Upload_HappyPath(t *testing.T) {
	svc, app := setupTestDataService(t) // 复用 meta_test.go 里的 setup

	// 1. 构造请求序列
	// Frame 1: Meta
	// Frame 2: Data Chunk
	data := []byte("hello grpc world")
	hashBytes := sha256.Sum256(data)
	linearHash := hex.EncodeToString(hashBytes[:])
	req1 := &tvrpc.UploadRequest{
		Payload: &tvrpc.UploadRequest_Meta{
			Meta: &tvrpc.FileMeta{
				Path:   "test.txt",
				Sha256: linearHash, // [关键]
			},
		},
	}
	req2 := &tvrpc.UploadRequest{
		Payload: &tvrpc.UploadRequest_ChunkData{
			ChunkData: data,
		},
	}

	stream := &MockUploadStream{
		Requests: []*tvrpc.UploadRequest{req1, req2},
	}

	// 2. 执行上传
	err := svc.Upload(stream)
	require.NoError(t, err)

	// 3. 验证响应
	require.NotNil(t, stream.Response)
	_ = core.CalculateBlobHash(data) // 因为只有一块，FileNode Hash = Hash(FileNode{Chunk})，这里略复杂，我们直接验证 TotalSize
	assert.NotEmpty(t, stream.Response.Hash)
	assert.Equal(t, int64(len(data)), stream.Response.TotalSize)

	// 4. 验证数据落地
	exists, err := app.Store.Has(context.Background(), types.Hash(stream.Response.Hash))
	require.NoError(t, err)
	assert.True(t, exists, "FileNode should be in store")

	idx, err := app.Repository.GetFileIndex(context.Background(), types.Hash(linearHash))
	require.NoError(t, err)
	require.NotNil(t, idx, "Index should be created after successful upload")
	assert.Equal(t, stream.Response.Hash, idx.MerkleRoot.String())
}

func TestDataService_Upload_ProtocolViolation(t *testing.T) {
	svc, _ := setupTestDataService(t)

	// 错误场景：第一帧不是 Meta，而是数据
	req := &tvrpc.UploadRequest{
		Payload: &tvrpc.UploadRequest_ChunkData{ChunkData: []byte("bad")},
	}
	stream := &MockUploadStream{Requests: []*tvrpc.UploadRequest{req}}

	err := svc.Upload(stream)
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code(), "Should reject data as first frame")
}

func TestDataService_Upload_IntegrityFail(t *testing.T) {
	svc, app := setupTestDataService(t)
	ctx := context.Background()

	// 1. 准备数据
	data := []byte("hello corrupted world")

	// 2. 故意构造一个【错误】的哈希
	// 真实哈希应该是 sha256("hello corrupted world")
	// 但我们传一个全 0 的假哈希
	fakeHash := "000000000000000000000000000000000000000000000000000000000000dead"

	// Frame 1: Meta (带错误的 Hash)
	req1 := &tvrpc.UploadRequest{
		Payload: &tvrpc.UploadRequest_Meta{
			Meta: &tvrpc.FileMeta{
				Path:   "test.txt",
				Sha256: fakeHash,
			},
		},
	}
	// Frame 2: Data
	req2 := &tvrpc.UploadRequest{
		Payload: &tvrpc.UploadRequest_ChunkData{
			ChunkData: data,
		},
	}

	stream := &MockUploadStream{
		Requests: []*tvrpc.UploadRequest{req1, req2},
	}

	// 3. 执行上传
	err := svc.Upload(stream)

	// 4. 验证结果：必须报错
	require.Error(t, err)

	// 5. 验证错误码：必须是 DataLoss
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.DataLoss, st.Code(), "Should return DataLoss when integrity check fails")
	assert.Contains(t, st.Message(), "integrity check failed")

	// 6. 验证副作用：索引表里不应该有这个假哈希的记录
	idx, err := app.Repository.GetFileIndex(ctx, types.Hash(fakeHash))
	require.NoError(t, err)
	assert.Nil(t, idx, "Index should NOT be created for corrupted upload")
}

func TestDataService_Download_HappyPath(t *testing.T) {
	svc, app := setupTestDataService(t)
	ctx := context.Background()

	// 1. 准备数据：先手动存一个文件进去
	data := []byte("downloadable content")
	chunk := core.NewChunk(data)
	require.NoError(t, app.Store.Put(ctx, chunk))

	fileNode, err := core.NewFileNode(int64(len(data)), []core.ChunkLink{core.NewChunkLink(chunk)})
	require.NoError(t, err)
	require.NoError(t, app.Store.Put(ctx, fileNode))
	targetHash := fileNode.ID()

	// 2. 构造下载请求
	req := &tvrpc.DownloadRequest{Hash: targetHash.String()}
	stream := &MockDownloadStream{}

	// 3. 执行下载
	err = svc.Download(req, stream)
	require.NoError(t, err)

	// 4. 验证收到的数据
	// 可能分成了多个 Chunk (取决于 Exporter 逻辑)，我们需要拼起来
	var received bytes.Buffer
	for _, resp := range stream.Responses {
		received.Write(resp.ChunkData)
	}

	assert.Equal(t, data, received.Bytes())
}

func TestDataService_Download_NotFound(t *testing.T) {
	svc, _ := setupTestDataService(t)

	req := &tvrpc.DownloadRequest{Hash: "1111222233334444555566667777888899990000aaaabbbbccccddddeeeeffff"} // 随便写的
	stream := &MockDownloadStream{}

	err := svc.Download(req, stream)
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.NotFound, st.Code())
}
