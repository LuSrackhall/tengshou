GO ?= go

.PHONY: test vet demo serve build build-windows clean

test:
	$(GO) test -count=1 ./internal/... ./cmd/tengshou-cli

vet:
	$(GO) vet ./internal/... ./cmd/tengshou-cli

demo:
	$(GO) run ./cmd/tengshou-cli demo --data-dir ./data

serve:
	$(GO) run ./cmd/tengshou-cli serve --data-dir ./data

build:
	./scripts/build.sh

build-windows:
	powershell -File scripts/build-windows.ps1

clean:
	rm -rf dist data
