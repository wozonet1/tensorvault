package server

import (
	"context"
	"log/slog"
	"runtime/debug"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// =============================================================================
// 1. Logging Interceptor (结构化日志)
// =============================================================================

// UnaryLoggingInterceptor 负责拦截普通请求 (MetaService)
func UnaryLoggingInterceptor(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	start := time.Now()

	// 调用业务逻辑
	resp, err := handler(ctx, req)

	duration := time.Since(start)
	logRPC("Unary", info.FullMethod, duration, err)

	return resp, err
}

// StreamLoggingInterceptor 负责拦截流式请求 (DataService)
func StreamLoggingInterceptor(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	start := time.Now()

	// 包装 ServerStream 以便（可选地）捕获更多流式细节，目前先直接透传
	err := handler(srv, ss)

	duration := time.Since(start)
	logRPC("Stream", info.FullMethod, duration, err)

	return err
}

// logRPC 统一的日志打印逻辑
// 使用 Go 1.21+ 标准库 slog，这是目前的最佳实践
func logRPC(kind, method string, duration time.Duration, err error) {
	// 提取 gRPC 状态码
	st, _ := status.FromError(err)
	code := st.Code()

	level := slog.LevelInfo
	if code != codes.OK {
		// 只有非 OK 的状态才视为警告/错误
		// NotFound 这种业务错误可以算 Warn，Internal 算 Error
		if code == codes.Internal || code == codes.Unknown {
			level = slog.LevelError
		} else {
			level = slog.LevelWarn
		}
	}

	slog.Log(context.Background(), level, "gRPC Request",
		slog.String("kind", kind),
		slog.String("method", method),
		slog.String("code", code.String()),
		slog.Duration("dur", duration),
		slog.String("err", errToString(err)),
	)
}

func errToString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// =============================================================================
// 2. Recovery Interceptor (防弹衣)
// =============================================================================

// UnaryRecoveryInterceptor 捕获 Panic
func UnaryRecoveryInterceptor(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = recoverFromPanic(r)
		}
	}()
	return handler(ctx, req)
}

// StreamRecoveryInterceptor 捕获 Panic
func StreamRecoveryInterceptor(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = recoverFromPanic(r)
		}
	}()
	return handler(srv, ss)
}

func recoverFromPanic(p any) error {
	// 打印堆栈信息，方便调试
	stack := string(debug.Stack())
	slog.Error("🔥 PANIC RECOVERED",
		slog.Any("panic", p),
		slog.String("stack", stack),
	)
	// 返回一个友好的 gRPC Internal 错误给客户端，而不是直接断开连接
	return status.Errorf(codes.Internal, "internal server error: panic recovered")
}
