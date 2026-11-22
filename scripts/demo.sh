#!/usr/bin/env bash
# 生成示例数据、签名图片与 PDF，并打印产物位置。
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
DATA_DIR="${1:-$ROOT/data}"
go run ./cmd/tengshou-cli demo --data-dir "$DATA_DIR"
