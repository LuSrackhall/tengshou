#!/usr/bin/env bash
# 前端结构预算棘轮：
#   1. 存量 internal/httpapi/web/app/app.js 只许减不许增（基线 1128 行，新手引导、模板与标定编辑器、合同详情生成面板、批量生成对话框与扫码签名对话框迁出后）；
#   2. 其余前端源文件（.js/.mjs/.cjs/.ts/.mts/.cts/.jsx/.tsx/.vue）单文件 ≤ 600 行。
# 排除构建产物、依赖、第三方 vendored 代码与并行 worktree。
# 用法：在仓库任意位置执行 `bash scripts/check-frontend-structure.sh`
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

APP_JS="internal/httpapi/web/app/app.js"
APP_JS_BASELINE=1128
MAX_LINES=600

fail=0

# 1) 存量 app.js 只许减不许增
if [ -f "$APP_JS" ]; then
  current="$(wc -l < "$APP_JS" | tr -d ' ')"
  if [ "$current" -gt "$APP_JS_BASELINE" ]; then
    echo "FAIL: $APP_JS 现有 $current 行，超过基线 ${APP_JS_BASELINE} 行。" >&2
    echo "      迁移期存量 app.js 只许减不许增；新功能请落在 Vue 组件中。" >&2
    fail=1
  elif [ "$current" -lt "$APP_JS_BASELINE" ]; then
    echo "OK: $APP_JS $current 行（基线 ${APP_JS_BASELINE}，缩小后可下调基线）。"
  else
    echo "OK: $APP_JS $current 行（等于基线）。"
  fi
else
  echo "OK: $APP_JS 已不存在（存量迁移完成），跳过基线检查。"
fi

# 2) 新增前端源文件不得超过单文件上限
while IFS= read -r -d '' file; do
  if [ "$file" = "./$APP_JS" ]; then
    continue
  fi
  lines="$(wc -l < "$file" | tr -d ' ')"
  if [ "$lines" -gt "$MAX_LINES" ]; then
    echo "FAIL: $file 有 $lines 行，超过单文件上限 $MAX_LINES 行。" >&2
    fail=1
  fi
done < <(find . \
  \( -name .git -o -name dist -o -name node_modules -o -name vendor -o -name worktrees \) -prune -o \
  -type f \( -name '*.js' -o -name '*.mjs' -o -name '*.cjs' \
             -o -name '*.ts' -o -name '*.mts' -o -name '*.cts' \
             -o -name '*.jsx' -o -name '*.tsx' -o -name '*.vue' \) -print0)

if [ "$fail" -ne 0 ]; then
  echo "前端结构检查未通过。" >&2
  exit 1
fi
echo "前端结构检查通过。"
