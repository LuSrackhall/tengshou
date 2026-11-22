GO ?= go
NPM ?= npm
FRONTEND_DIR ?= frontend

.PHONY: test vet demo serve build build-windows clean frontend

test:
	$(GO) test -count=1 ./internal/... ./cmd/tengshou-cli

vet:
	$(GO) vet ./internal/... ./cmd/tengshou-cli

# 前端构建：Vite 产物写入 internal/httpapi/web/app/dist，由 go:embed 打包。
# 依赖按 package-lock.json 安装；node_modules 已存在时跳过重复 npm ci。
frontend:
	cd $(FRONTEND_DIR) && if [ ! -d node_modules ]; then $(NPM) ci; fi && $(NPM) run build

demo:
	$(GO) run ./cmd/tengshou-cli demo --data-dir ./data

serve: frontend
	$(GO) run ./cmd/tengshou-cli serve --data-dir ./data

build: frontend
	./scripts/build.sh

build-windows: frontend
	powershell -File scripts/build-windows.ps1

clean:
	rm -rf dist data
