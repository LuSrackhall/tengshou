#!/usr/bin/env bash
# 发布形态冒烟：以真实 agent 方式（stdin/stdout 管道）拉起桌面二进制的
# `mcp` 子命令，验证桌面壳（macOS 桌面二进制；Windows 对应脚本见
# scripts/smoke-mcp-desktop.ps1）也能提供 stdio MCP，而不是只有 CLI 能跑。
#
# 用法：scripts/smoke-mcp-desktop.sh <桌面二进制> <tengshou-cli>
#
# 步骤：
#   1. 本地服务尚未启动时拉起 `<桌面二进制> mcp`，断言 initialize / tools/list；
#   2. 启动同版 `tengshou-cli serve`，调用一次写工具 tengshou_create_field 成功。
#
# 依赖：bash、curl、python3（均随 CI runner / macOS 开发者工具提供）。
set -euo pipefail

DESKTOP="${1:?用法：scripts/smoke-mcp-desktop.sh <桌面二进制> <tengshou-cli>}"
CLI="${2:?用法：scripts/smoke-mcp-desktop.sh <桌面二进制> <tengshou-cli>}"
for exe in "$DESKTOP" "$CLI"; do
  if [ ! -x "$exe" ]; then
    echo "错误：找不到可执行文件 $exe" >&2
    exit 1
  fi
done
if ! command -v python3 >/dev/null 2>&1; then
  echo "错误：未找到 python3" >&2
  exit 1
fi

TMP="$(mktemp -d "${RUNNER_TEMP:-/tmp}/tengshou-mcp-smoke.XXXXXX")"
DATA="$TMP/data"
SERVE_PID=""
cleanup() {
  if [ -n "$SERVE_PID" ]; then
    kill "$SERVE_PID" 2>/dev/null || true
    wait "$SERVE_PID" 2>/dev/null || true
  fi
  rm -rf "$TMP"
}
trap cleanup EXIT

PORT="$(python3 -I -c 'import socket
s = socket.socket()
s.bind(("127.0.0.1", 0))
print(s.getsockname()[1])
s.close()')"

cat >"$TMP/init.jsonl" <<'JSON'
{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"release-smoke","version":"0"}}}
{"jsonrpc":"2.0","method":"notifications/initialized"}
{"jsonrpc":"2.0","id":2,"method":"tools/list"}
JSON

echo "1/2 桌面二进制 mcp：initialize / tools/list（本地服务未启动）"
"$DESKTOP" mcp --data-dir "$DATA" <"$TMP/init.jsonl" >"$TMP/init.out" 2>"$TMP/init.err"
python3 -I - "$TMP/init.out" <<'PY'
import json
import sys

lines = [line for line in open(sys.argv[1], encoding="utf-8") if line.strip()]
if len(lines) != 2:
    raise SystemExit(f"错误：期望 2 条 JSON-RPC 响应，实际 {len(lines)} 条：{lines}")
init = json.loads(lines[0])
if init.get("result", {}).get("serverInfo", {}).get("name") != "tengshou":
    raise SystemExit(f"错误：initialize 返回异常：{lines[0]}")
tools = {tool["name"] for tool in json.loads(lines[1]).get("result", {}).get("tools", [])}
for want in ("tengshou_status", "tengshou_create_field"):
    if want not in tools:
        raise SystemExit(f"错误：tools/list 缺少 {want}：{sorted(tools)}")
print(f"   initialize / tools/list 通过（{len(tools)} 个工具）")
PY

echo "2/2 启动 tengshou-cli serve 后经桌面二进制调用写工具"
"$CLI" serve --data-dir "$DATA" --port "$PORT" --loopback >"$TMP/serve.log" 2>&1 &
SERVE_PID=$!
ready=0
for _ in $(seq 1 100); do
  if curl -fsS "http://127.0.0.1:$PORT/api/v1/health" >/dev/null 2>&1; then
    ready=1
    break
  fi
  if ! kill -0 "$SERVE_PID" 2>/dev/null; then
    break
  fi
  sleep 0.2
done
if [ "$ready" != 1 ]; then
  echo "错误：本地服务未就绪" >&2
  cat "$TMP/serve.log" >&2 || true
  exit 1
fi

cat >"$TMP/write.jsonl" <<'JSON'
{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"release-smoke","version":"0"}}}
{"jsonrpc":"2.0","method":"notifications/initialized"}
{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"tengshou_create_field","arguments":{"entity":"contract","key":"smoke_field","label":"smoke field","type":"text"}}}
JSON

"$DESKTOP" mcp --data-dir "$DATA" --api-url "http://127.0.0.1:$PORT" \
  <"$TMP/write.jsonl" >"$TMP/write.out" 2>"$TMP/write.err"
python3 -I - "$TMP/write.out" <<'PY'
import json
import sys

lines = [line for line in open(sys.argv[1], encoding="utf-8") if line.strip()]
if len(lines) != 2:
    raise SystemExit(f"错误：期望 2 条 JSON-RPC 响应，实际 {len(lines)} 条：{lines}")
response = json.loads(lines[1])
result = response.get("result") or {}
if result.get("isError"):
    raise SystemExit(f"错误：写工具返回 isError：{lines[1]}")
text = (result.get("content") or [{}])[0].get("text", "")
if "smoke_field" not in text:
    raise SystemExit(f"错误：写工具响应缺少字段 smoke_field：{lines[1]}")
print("   写工具 tengshou_create_field 通过")
PY

echo "冒烟通过：桌面二进制 mcp（stdio 管道）+ tengshou-cli serve 写工具"
