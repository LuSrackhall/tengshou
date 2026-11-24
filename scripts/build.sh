#!/usr/bin/env bash
# 构建桌面壳与无界面 CLI；CI 与本地共用本脚本，发布产物只能由这组 flags 构建。
#
# 用法：scripts/build.sh [desktop|cli|all]（默认 all）
#
# 环境变量：
#   VERSION       版本号（默认 git describe --tags --abbrev=0，否则 0.1.0-dev）
#   COMMIT        提交号（默认 git rev-parse --short HEAD）
#   BUILD_DATE    构建时间（默认当前 UTC 时间）
#   OUT           输出目录（默认 dist）
#   GOOS/GOARCH   目标平台（默认本机）
#   CGO_ENABLED   cgo 开关（默认 go 环境值；macOS 桌面壳需要 1）
#   NAME_SUFFIX   产物名后缀，例如 -darwin-arm64（默认空，产物为 tengshou / tengshou-cli）
#
# Windows 目标（桌面 GUI 子系统 / CLI 控制台子系统）统一走
# scripts/build-windows.ps1：两个子系统 flags 只在那一处维护，避免 dev 与 CI
# 各写一份而漂移。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

TARGET="${1:-all}"
case "$TARGET" in
  desktop | cli | all) ;;
  *)
    echo "用法：scripts/build.sh [desktop|cli|all]" >&2
    exit 2
    ;;
esac

GOOS="${GOOS:-$(go env GOOS)}"
GOARCH="${GOARCH:-$(go env GOARCH)}"
CGO_ENABLED="${CGO_ENABLED:-$(go env CGO_ENABLED)}"
export GOOS GOARCH CGO_ENABLED

if [ "$GOOS" = "windows" ]; then
  echo "错误：Windows 构建请用 scripts/build-windows.ps1（子系统 flags 收口在那里）" >&2
  exit 1
fi

VERSION="${VERSION:-$(git describe --tags --abbrev=0 2>/dev/null || echo 0.1.0-dev)}"
COMMIT="${COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || echo unknown)}"
BUILD_DATE="${BUILD_DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
OUT="${OUT:-dist}"
NAME_SUFFIX="${NAME_SUFFIX:-}"

LDFLAGS=(
  -s -w
  -X "github.com/LuSrackhall/tengshou/internal/version.Version=${VERSION}"
  -X "github.com/LuSrackhall/tengshou/internal/version.Commit=${COMMIT}"
  -X "github.com/LuSrackhall/tengshou/internal/version.BuildDate=${BUILD_DATE}"
)

mkdir -p "$OUT"

if [ "$TARGET" = "desktop" ] || [ "$TARGET" = "all" ]; then
  echo "构建桌面应用 ${GOOS}/${GOARCH} ..."
  go build -trimpath -ldflags "${LDFLAGS[*]}" \
    -o "$OUT/tengshou${NAME_SUFFIX}" ./cmd/tengshou
fi

if [ "$TARGET" = "cli" ] || [ "$TARGET" = "all" ]; then
  echo "构建 tengshou-cli ${GOOS}/${GOARCH} ..."
  go build -trimpath -ldflags "${LDFLAGS[*]}" \
    -o "$OUT/tengshou-cli${NAME_SUFFIX}" ./cmd/tengshou-cli
fi

echo "完成："
ls -lh "$OUT"
echo
echo "运行示例："
echo "  $OUT/tengshou-cli${NAME_SUFFIX} demo --data-dir ./data"
echo "  $OUT/tengshou-cli${NAME_SUFFIX} serve --data-dir ./data"
echo "  $OUT/tengshou${NAME_SUFFIX} mcp --data-dir ./data"
