#!/usr/bin/env bash
# 组装 Windows 分发包：桌面应用 + 接入说明，共 2 个文件。
# CI 发布与本地复现共用本脚本，保证两边打出的 zip 内容一致。
#
# 客户只拿桌面客户端一个 exe：它同时提供 GUI 与 `mcp` stdio 子命令。
# 完整 CLI（serve / backup / restore / demo 等高级用途）仍作为独立发布资产，
# 不再放进 zip。
#
# 用法：scripts/package-windows-zip.sh [dist 目录]
#
# dist 目录需包含：
#   tengshou-windows-amd64.exe       桌面应用（内置 MCP 连接器）
# 产出：
#   <dist>/tengshou-windows-amd64.zip
#
# 用 Python zipfile 而不是 zip(1)：中文文件名需要置上 ZIP 通用标志位
# 0x800（UTF-8），否则 Windows 资源管理器会按本地代码页显示成乱码。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT_DIR="${1:-$ROOT/dist}"
ZIP_NAME="tengshou-windows-amd64.zip"
FILES=(
  "tengshou-windows-amd64.exe"
)
README_NAME="MCP-接入.txt"
README_SRC="$ROOT/packaging/windows/$README_NAME"

for name in "${FILES[@]}"; do
  if [ ! -f "$OUT_DIR/$name" ]; then
    echo "错误：缺少打包输入 $OUT_DIR/$name" >&2
    exit 1
  fi
done
if [ ! -f "$README_SRC" ]; then
  echo "错误：缺少接入说明 $README_SRC" >&2
  exit 1
fi
PYTHON="$(command -v python3 || command -v python || true)"
if [ -z "$PYTHON" ]; then
  echo "错误：未找到 python3 / python（CI runner 与 macOS 开发者工具均自带）" >&2
  exit 1
fi

mkdir -p "$OUT_DIR"
OUT_DIR="$(cd "$OUT_DIR" && pwd)"
ZIP_PATH="$OUT_DIR/$ZIP_NAME"
rm -f "$ZIP_PATH"

"$PYTHON" -I - "$ZIP_PATH" \
  "$OUT_DIR/${FILES[0]}" "${FILES[0]}" \
  "$README_SRC" "$README_NAME" <<'PY'
import sys
import zipfile

zip_path = sys.argv[1]
pairs = list(zip(sys.argv[2::2], sys.argv[3::2]))
expected = sorted(name for _, name in pairs)
with zipfile.ZipFile(zip_path, "w", compression=zipfile.ZIP_DEFLATED) as z:
    for src, name in pairs:
        z.write(src, arcname=name)

with zipfile.ZipFile(zip_path) as z:
    names = sorted(z.namelist())
if names != expected:
    raise SystemExit(f"错误：zip 内容不符：{names} != {expected}")
PY

echo "已生成 $ZIP_PATH"
echo "包含文件："
printf '  %s\n' "${FILES[@]}" "$README_NAME"
if command -v unzip >/dev/null 2>&1; then
  unzip -l "$ZIP_PATH"
fi
