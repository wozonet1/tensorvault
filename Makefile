# Makefile
cover:
	go test ./pkg/... -coverprofile=coverage.out
	go tool cover -func=coverage.out # 在终端显示总计
	@echo "Run 'go tool cover -html=coverage.out' to see details."