GO ?= go
NPM ?= npm
FRONTEND_DIR ?= frontend

.PHONY: test vet demo serve build build-windows clean frontend

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

clean:
	rm -rf dist data
