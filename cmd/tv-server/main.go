package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	tvrpc "tensorvault/pkg/api/tvrpc/v1"
	"tensorvault/pkg/app"
	"tensorvault/pkg/config"
	"tensorvault/pkg/server"
	"tensorvault/pkg/service"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

const DefaultPort = ":8080"

func main() {
	// 1. Load Config (支持 -config 参数)
	cfgFile := flag.String("config", "", "config file (default is $HOME/.tv/config.yaml)")
	flag.Parse()

	if err := config.Load(*cfgFile); err != nil {
		log.Fatalf("❌ Config error: %v", err)
	}

	// 2. Init Core Application (依赖注入容器)
	// 这里会初始化 DB、S3、Redis 和 Index
	application, err := app.NewApp()
	if err != nil {
		log.Fatalf("❌ Failed to initialize app: %v", err)
	}
	fmt.Println("✅ TensorVault Core initialized.")

	// 3. Setup Network
	lis, err := net.Listen("tcp", DefaultPort)
	if err != nil {
		log.Fatalf("❌ Failed to listen on %s: %v", DefaultPort, err)
	}

	// 4. Setup gRPC Server
	// 可以在这里添加拦截器 (Interceptors) 用于日志或鉴权
	grpcServer := grpc.NewServer( // 挂载 Unary (MetaService)
		grpc.ChainUnaryInterceptor(
			server.UnaryRecoveryInterceptor,
			server.UnaryLoggingInterceptor,
		),
		// 挂载 Stream (DataService)
		grpc.ChainStreamInterceptor(
			server.StreamRecoveryInterceptor,
			server.StreamLoggingInterceptor,
		))

	// 5. 注册服务 (Wiring Services)
	// A. MetaService (Unary)
	metaSvc := service.NewMetaService(application)
	tvrpc.RegisterMetaServiceServer(grpcServer, metaSvc)

	// B. DataService (Streaming) - [新增]
	dataSvc := service.NewDataService(application)
	tvrpc.RegisterDataServiceServer(grpcServer, dataSvc)

	// 6. Enable Reflection
	// 允许使用 grpcurl 等工具调试
	reflection.Register(grpcServer)

	// 7. Start Server (Async)
	go func() {
		fmt.Printf("🚀 gRPC Server listening on %s...\n", DefaultPort)
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatalf("❌ Failed to serve: %v", err)
		}
	}()

	// 8. Graceful Shutdown
	// 监听中断信号，确保所有正在传输的流完成后再关闭
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	fmt.Println("\n⚠️  Shutting down server...")
	// 创建一个带超时的 Context (例如 30秒)
	// 这是给正在传输的文件留出的最后时间窗口
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 启动一个 goroutine 来执行 GracefulStop
	done := make(chan struct{})
	go func() {
		grpcServer.GracefulStop() // 这会阻塞，直到所有 RPC 结束
		close(done)
	}()

	select {
	case <-done:
		fmt.Println("✅ Server stopped gracefully.")
	case <-ctx.Done():
		fmt.Println("⏳ Timeout reached. Forcing shutdown...")
		grpcServer.Stop() // 强制关闭，断开所有连接
	}
}
