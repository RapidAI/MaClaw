.PHONY: check-corelib-deps build-corelib-headless build-tui build-gui build-tool build-all test clean check-bindings

# 输出目录
BIN_DIR := bin

# Wails 绑定同步检查：Go App 导出方法必须在 JS/TS 绑定文件中有对应声明
check-bindings:
	@echo "Checking Wails binding sync..."
	go run scripts/check_wails_bindings.go
	@echo "OK: bindings in sync"

# 依赖隔离检查：corelib 不得引用 wails/systray
check-corelib-deps:
	@echo "Checking corelib has no GUI dependencies..."
	@! grep -r "wailsapp/wails" corelib/ || (echo "FAIL: corelib imports wails" && exit 1)
	@! grep -r "energye/systray" corelib/ || (echo "FAIL: corelib imports systray" && exit 1)
	@echo "OK: corelib is GUI-free"

# corelib 无头交叉编译验证
build-corelib-headless:
	@echo "Building corelib (linux/amd64)..."
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build ./corelib/...
	@echo "Building corelib (darwin/arm64)..."
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build ./corelib/...
	@echo "OK: corelib headless build passed"

# TUI 编译（CGO_ENABLED=0，可交叉编译）
build-tui:
	@echo "Building maclaw-tui..."
	go build -o $(BIN_DIR)/maclaw-tui ./tui/
	@echo "OK: $(BIN_DIR)/maclaw-tui"

# GUI 编译（需要 CGO + Wails）
build-gui:
	@echo "Building maclaw-gui..."
	go build -o $(BIN_DIR)/maclaw-gui ./gui/
	@echo "OK: $(BIN_DIR)/maclaw-gui"

# maclaw-tool 编译
build-tool:
	@echo "Building maclaw-tool..."
	go build -o $(BIN_DIR)/maclaw-tool ./cmd/maclaw-tool/
	@echo "OK: $(BIN_DIR)/maclaw-tool"

# 全量编译
build-all: check-corelib-deps check-bindings build-corelib-headless build-tui build-gui build-tool
	@echo "All builds passed."

# 运行测试
test:
	go test ./...

# 清理编译产物
clean:
	rm -rf $(BIN_DIR)
