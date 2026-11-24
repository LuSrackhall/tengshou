// MCP stdio 客户端：以子进程运行 tengshou-cli mcp，按行收发 JSON-RPC 2.0。
//
// 与真实接入方式一致：--api-url 指向同一台 serve 实例，写工具经 loopback
// 应用写入面落到服务，变更记录 actor=tengshou-mcp，界面据此回放 agent 操作。
import { spawn } from "node:child_process";
import { createInterface } from "node:readline";
import type { AppHandle } from "./app";

interface RPCResponse {
  id?: number;
  result?: unknown;
  error?: { code: number; message: string };
}

interface MCPToolResult {
  content?: Array<{ type?: string; text?: string }>;
  isError?: boolean;
}

const REQUEST_TIMEOUT_MS = 20_000;

/** 调用一个 MCP 工具；成功返回工具文本，失败抛错并带上 stderr 便于定位。 */
export async function callMCPTool(
  app: AppHandle,
  name: string,
  args: Record<string, unknown>,
): Promise<string> {
  const cliArgs = ["mcp", "--api-url", app.baseURL, "--token", app.token];
  if (app.dataDir) {
    cliArgs.push("--data-dir", app.dataDir);
  }
  const child = spawn(app.cli, cliArgs, {
    stdio: ["pipe", "pipe", "pipe"],
    windowsHide: true,
  });
  child.stdout.setEncoding("utf8");
  child.stderr.setEncoding("utf8");

  let stderr = "";
  child.stderr.on("data", (chunk: string) => {
    stderr += chunk;
  });

  const pending = new Map<number, (response: RPCResponse) => void>();
  const lines = createInterface({ input: child.stdout });
  lines.on("line", (line) => {
    let parsed: RPCResponse;
    try {
      parsed = JSON.parse(line) as RPCResponse;
    } catch {
      return;
    }
    if (typeof parsed.id !== "number") {
      return;
    }
    const resolve = pending.get(parsed.id);
    if (resolve) {
      pending.delete(parsed.id);
      resolve(parsed);
    }
  });

  const send = (payload: object): void => {
    child.stdin.write(`${JSON.stringify(payload)}\n`);
  };

  const request = (
    id: number,
    method: string,
    params?: unknown,
  ): Promise<RPCResponse> =>
    new Promise<RPCResponse>((resolve, reject) => {
      const timer = setTimeout(() => {
        pending.delete(id);
        reject(
          new Error(`MCP ${method} 超时（${REQUEST_TIMEOUT_MS}ms）：\n${stderr}`),
        );
      }, REQUEST_TIMEOUT_MS);
      pending.set(id, (response) => {
        clearTimeout(timer);
        resolve(response);
      });
      send(
        params === undefined
          ? { jsonrpc: "2.0", id, method }
          : { jsonrpc: "2.0", id, method, params },
      );
    });

  try {
    const init = await request(1, "initialize", {
      protocolVersion: "2025-06-18",
      capabilities: {},
      clientInfo: { name: "tengshou-e2e", version: "1.0.0" },
    });
    if (init.error) {
      throw new Error(`MCP initialize 失败：${init.error.message}\n${stderr}`);
    }
    send({ jsonrpc: "2.0", method: "notifications/initialized" });

    const response = await request(2, "tools/call", {
      name,
      arguments: args,
    });
    if (response.error) {
      throw new Error(`MCP tools/call ${name} 失败：${response.error.message}\n${stderr}`);
    }
    const result = (response.result ?? {}) as MCPToolResult;
    const text = (result.content ?? [])
      .map((item) => item.text ?? "")
      .join("\n");
    if (result.isError) {
      throw new Error(`MCP 工具 ${name} 返回错误：${text}\n${stderr}`);
    }
    return text;
  } finally {
    pending.clear();
    lines.close();
    child.stdin.end();
    child.kill();
  }
}
