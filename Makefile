GO ?= go
NPM ?= npm
FRONTEND_DIR ?= frontend

.PHONY: test vet demo serve build build-windows clean frontend e2e

test:
	$(GO) test -count=1 ./internal/... ./cmd/tengshou-cli ./cmd/tengshou ./tools/...

vet:
	$(GO) vet ./internal/... ./cmd/tengshou-cli ./cmd/tengshou ./tools/...

# 前端构建：Vite 产物写入 internal/httpapi/web/app/dist，由 go:embed 打包。
# 依赖按 package-lock.json 安装；node_modules 已存在时跳过重复 npm ci。
frontend:
	cd $(FRONTEND_DIR) && if [ ! -d node_modules ]; then $(NPM) ci; fi && $(NPM) run build

demo:
	$(GO) run ./cmd/tengshou-cli demo --data-dir ./data

serve: frontend
	$(GO) run ./cmd/tengshou-cli serve --data-dir ./data

build: frontend
	$(GO) run ./tools/tengshoubuild build

# 跨平台目标用 -goos/-goarch 参数（go run 不能按目标平台交叉编译工具本身）。
build-windows: frontend
	$(GO) run ./tools/tengshoubuild build -goos windows -goarch amd64

# 界面端到端测试：先构建发布形态 CLI（go:embed 同一份界面资源），
# 再由 Playwright 在临时数据目录拉起 serve。首次运行前：
#   cd e2e && npm ci && npx playwright install
# （Linux/Windows 用 chromium，macOS 用 webkit，见 docs/e2e.md）
e2e: frontend
	$(GO) run ./tools/tengshoubuild build cli
	cd e2e && if [ ! -d node_modules ]; then $(NPM) ci; fi && $(NPM) test

clean:
	rm -rf dist data
