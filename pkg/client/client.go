package client

import (
	"fmt"
	"time"

	tvrpc "tensorvault/pkg/api/tvrpc/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

// TVClient 封装了与 TensorVault 服务端的连接
type TVClient struct {
	conn *grpc.ClientConn

	// 公开具体的 Service Client
	Data tvrpc.DataServiceClient
	Meta tvrpc.MetaServiceClient
}

// NewTVClient 创建并初始化客户端
// 注意：这里不再需要 context，因为它只负责创建对象，不负责等待连接就绪
func NewTVClient(addr string) (*TVClient, error) {
	// 配置选项
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(1024*1024*1024), // 1GB
			grpc.MaxCallSendMsgSize(1024*1024*1024), // 1GB
		),
		// [新增] 保持连接活跃
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                10 * time.Second,
			Timeout:             20 * time.Second,
			PermitWithoutStream: true,
		}),
	}

	// [核心变更] 使用 NewClient 替代 DialContext
	// 它会立即返回，连接在后台进行
	conn, err := grpc.NewClient(addr, opts...)
	if err != nil {
		// 注意：这里的 err 通常只是配置错误（如地址格式不对）
		// 网络不通不会在这里报错
		return nil, fmt.Errorf("failed to create grpc client for %s: %w", addr, err)
	}

	return &TVClient{
		conn: conn,
		Data: tvrpc.NewDataServiceClient(conn),
		Meta: tvrpc.NewMetaServiceClient(conn),
	}, nil
}

// Close 关闭底层连接
func (c *TVClient) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}
