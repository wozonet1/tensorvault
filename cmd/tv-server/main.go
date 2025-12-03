package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	// 引入生成的代码和内部包
	tvrpc "tensorvault/pkg/api/tvrpc/v1"
	"tensorvault/pkg/app"
	"tensorvault/pkg/config"
	"tensorvault/pkg/service"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

const (
	DefaultPort = ":8080"
)

func main() {
	// 0. 解析命令行参数 (比 Cobra 轻量)
	cfgFile := flag.String("config", "", "config file (default is $HOME/.tv/config.yaml)")
	flag.Parse()

	// 1. 加载配置 (The Missing Piece)
	if err := config.Load(*cfgFile); err != nil {
		log.Fatalf("❌ Failed to load config: %v", err)
	}

	// 2. 初始化基础设施
	application, err := app.NewApp()
	if err != nil {
		log.Fatalf("❌ Failed to initialize application: %v", err)
	}
	fmt.Println("✅ TensorVault Core initialized (DB+S3+Redis connected).")

	// 2. 监听网络端口
	lis, err := net.Listen("tcp", DefaultPort)
	if err != nil {
		log.Fatalf("❌ Failed to listen on %s: %v", DefaultPort, err)
	}

	// 3. 创建 gRPC Server 实例
	// 这里未来可以添加 Interceptor (拦截器)，如日志、鉴权、Panic恢复
	grpcServer := grpc.NewServer()

	// 4. 注册服务 (Service Layer)
	// 将我们的 Go 结构体 (MetaService) 绑定到 gRPC 协议上
	metaSvc := service.NewMetaService(application)
	tvrpc.RegisterMetaServiceServer(grpcServer, metaSvc)

	// TODO: 下一步注册 DataService
	// dataSvc := service.NewDataService(application)
	// tvrpc.RegisterDataServiceServer(grpcServer, dataSvc)

	// 5. 启用反射 (Server Reflection)
	// 【架构师提示】这是一个开发神器。它允许 grpcurl 等工具动态获取服务的方法列表。
	// 生产环境为了安全通常会关闭，但内网微服务建议开启。
	reflection.Register(grpcServer)

	// 6. 启动服务 (带优雅退出)
	go func() {
		fmt.Printf("🚀 gRPC Server listening on %s...\n", DefaultPort)
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatalf("❌ Failed to serve: %v", err)
		}
	}()

	// 7. 优雅退出 (Graceful Shutdown)
	// 监听 Ctrl+C (SIGINT) 或 kill (SIGTERM)
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit // 阻塞直到收到信号

	fmt.Println("\n⚠️  Shutting down server...")
	// GracefulStop 会等待当前正在处理的请求完成后再停止，这对于数据一致性至关重要
	grpcServer.GracefulStop()
	fmt.Println("👋 Server stopped.")
}
