#!/usr/bin/env bash
# 前端结构预算检查：
#   1. 存量 internal/httpapi/web/app/app.js 已退役（阶段 1 收口），不得重新出现；
#   2. 前端源文件（.js/.mjs/.cjs/.ts/.mts/.cts/.jsx/.tsx/.vue）单文件 ≤ 600 行。
# 排除构建产物、依赖、第三方 vendored 代码与并行 worktree。
# 用法：在仓库任意位置执行 `bash scripts/check-frontend-structure.sh`
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

APP_JS="internal/httpapi/web/app/app.js"
MAX_LINES=600

fail=0

# 1) 存量 app.js 已退役，禁止回退到旧入口
if [ -f "$APP_JS" ]; then
  echo "FAIL: $APP_JS 已退役，界面应全部落在 frontend/src/ 的 Vue 组件中。" >&2
  fail=1
else
  echo "OK: $APP_JS 不存在（存量迁移完成）。"
fi

# 2) 前端源文件不得超过单文件上限
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
