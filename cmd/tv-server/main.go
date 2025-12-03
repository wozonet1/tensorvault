package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	tvrpc "tensorvault/pkg/api/tvrpc/v1"
	"tensorvault/pkg/app"
	"tensorvault/pkg/config"
	"tensorvault/pkg/service"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

const DefaultPort = ":8080"

func main() {
	// 1. Load Config
	cfgFile := flag.String("config", "", "config file (default is $HOME/.tv/config.yaml)")
	flag.Parse()

	if err := config.Load(*cfgFile); err != nil {
		log.Fatalf("❌ Config error: %v", err)
	}

	// 2. Init Core Application
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
	grpcServer := grpc.NewServer()

	// Register Services
	metaSvc := service.NewMetaService(application)
	tvrpc.RegisterMetaServiceServer(grpcServer, metaSvc)

	// Enable Reflection for debugging tools (grpcurl)
	reflection.Register(grpcServer)

	// 5. Start Server (Async)
	go func() {
		fmt.Printf("🚀 gRPC Server listening on %s...\n", DefaultPort)
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatalf("❌ Failed to serve: %v", err)
		}
	}()

	// 6. Graceful Shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	fmt.Println("\n⚠️  Shutting down server...")
	grpcServer.GracefulStop()
	fmt.Println("👋 Server stopped.")
}
