# Makefile

.PHONY: all test clean coverage lint integration-test

# 默认目标
all: lint test build

# 1. 基础单元测试 (跳过慢速测试)
test:
	@echo ">> Running Unit Tests..."
	@go test -race -short ./pkg/... ./cmd/...

# 2. 生成覆盖率报告 (你现在用的)
coverage:
	@echo ">> Generating Coverage..."
	
	# 1. 使用 go list 列出所有包
	# 2. 使用 grep -v 排除掉 api, proto, mock 等生成的代码目录
	# 3. 将过滤后的包列表传给 go test
	@go test -coverprofile=coverage.out $$(go list ./pkg/... ./cmd/... | grep -v "pkg/api" | grep -v "mock_")
	
	@go tool cover -func=coverage.out | grep total
	@go tool cover -html=coverage.out -o coverage.html
	@echo "Open coverage.html to view details (Generated code excluded)."

# 3. 集成测试 (E2E)
# 只有在明确需要时才跑，模拟真实环境
integration-test:
	@echo ">> Running Integration Tests..."
	@go test -tags=integration -count=1 ./cmd/... 

# 4. 代码静态分析 (Lint) - 极其重要！
lint:
	@echo ">> Linting code..."
	@golangci-lint run ./...

# 5. 清理垃圾
clean:
	@rm -f coverage.out coverage.html tensorvault
	@go clean -testcache