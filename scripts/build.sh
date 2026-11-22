#!/usr/bin/env bash
# Build the desktop shell and headless CLI for the current platform.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

VERSION="${VERSION:-$(git describe --tags --abbrev=0 2>/dev/null || echo 0.1.0-dev)}"
COMMIT="${COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || echo unknown)}"
BUILD_DATE="${BUILD_DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
OUT="${OUT:-dist}"

LDFLAGS=(
  -s -w
  -X "github.com/LuSrackhall/tengshou/internal/version.Version=${VERSION}"
  -X "github.com/LuSrackhall/tengshou/internal/version.Commit=${COMMIT}"
  -X "github.com/LuSrackhall/tengshou/internal/version.BuildDate=${BUILD_DATE}"
)

mkdir -p "$OUT"
echo "构建 tengshou-cli ..."
go build -trimpath -ldflags "${LDFLAGS[*]}" -o "$OUT/tengshou-cli" ./cmd/tengshou-cli

echo "构建桌面应用 tengshou ..."
go build -trimpath -ldflags "${LDFLAGS[*]}" -o "$OUT/tengshou" ./cmd/tengshou

echo "完成："
ls -lh "$OUT"
echo
echo "运行示例："
echo "  $OUT/tengshou-cli demo --data-dir ./data"
echo "  $OUT/tengshou-cli serve --data-dir ./data"
