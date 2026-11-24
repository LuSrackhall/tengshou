// 发布形态服务的启停夹具。
//
// 两种模式：
//   1. 设置 PLAYWRIGHT_BASE_URL：复用外部已启动的 tengshou-cli serve
//      （MCP 场景仍需要 TENGHSHOU_CLI 或 dist/ 里的 CLI 构建产物）。
//   2. 未设置：用空闲端口 + 临时数据目录自行拉起 dist/ 里的
//      tengshou-cli serve，测试结束回收进程与临时目录。
//
// 测试只驱动 release 形态（go:embed 的同一份界面资源），不连 Vite dev server。
import { spawn, type ChildProcess } from "node:child_process";
import { existsSync } from "node:fs";
import { mkdtemp, readFile, rm } from "node:fs/promises";
import { createServer } from "node:net";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

export interface AppHandle {
  baseURL: string;
  /** 只读数据接口令牌（MCP 连接器读取工具需要）。 */
  token: string;
  /** 临时数据目录；外部模式未提供 TENGHSHOU_DATA_DIR 时为 null。 */
  dataDir: string | null;
  /** CLI 可执行文件路径（MCP 场景用 `mcp` 子命令）。 */
  cli: string;
  stop: () => Promise<void>;
}

export function repoRoot(): string {
  return fileURLToPath(new URL("../../..", import.meta.url));
}

function platformName(): string {
  if (process.platform === "win32") {
    return "windows";
  }
  return process.platform === "darwin" ? "darwin" : "linux";
}

function archName(): string {
  return process.arch === "arm64" ? "arm64" : "amd64";
}

/** 解析 CLI：优先 TENGHSHOU_CLI，其次 dist/ 下三端同一命名规则。 */
export function locateCLI(): string {
  const explicit = process.env.TENGHSHOU_CLI?.trim();
  if (explicit) {
    if (!existsSync(explicit)) {
      throw new Error(`TENGHSHOU_CLI 指向的文件不存在：${explicit}`);
    }
    return explicit;
  }
  const ext = process.platform === "win32" ? ".exe" : "";
  const candidates = [
    join(repoRoot(), "dist", `tengshou-cli-${platformName()}-${archName()}${ext}`),
    join(repoRoot(), "dist", `tengshou-cli${ext}`),
  ];
  for (const candidate of candidates) {
    if (existsSync(candidate)) {
      return candidate;
    }
  }
  throw new Error(
    "找不到 tengshou-cli 构建产物。先运行 `go run ./tools/tengshoubuild build cli`，" +
      `或设置 TENGHSHOU_CLI。已尝试：${candidates.join("、")}`,
  );
}

async function freePort(): Promise<number> {
  return await new Promise<number>((resolve, reject) => {
    const server = createServer();
    server.unref();
    server.on("error", reject);
    server.listen(0, "127.0.0.1", () => {
      const address = server.address();
      const port =
        typeof address === "object" && address !== null ? address.port : 0;
      server.close(() => resolve(port));
    });
  });
}

async function readToken(dataDir: string): Promise<string> {
  const configPath = join(dataDir, "config.json");
  const deadline = Date.now() + 10_000;
  for (;;) {
    try {
      const raw = JSON.parse(await readFile(configPath, "utf8")) as {
        http?: { token?: string };
      };
      const token = raw.http?.token?.trim() ?? "";
      if (token) {
        return token;
      }
    } catch {
      /* 配置尚未落盘：继续等 */
    }
    if (Date.now() > deadline) {
      throw new Error(`读取 ${configPath} 的接口令牌超时`);
    }
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
}

async function bootstrapReady(baseURL: string): Promise<boolean> {
  try {
    const response = await fetch(`${baseURL}/app/api/v1/bootstrap`);
    return response.ok;
  } catch {
    return false;
  }
}

async function waitUntilReady(
  baseURL: string,
  child: ChildProcess | null,
  logs: () => string,
): Promise<void> {
  const deadline = Date.now() + 30_000;
  for (;;) {
    if (child && child.exitCode !== null) {
      throw new Error(
        `tengshou-cli serve 提前退出（code=${child.exitCode}）：\n${logs()}`,
      );
    }
    if (await bootstrapReady(baseURL)) {
      return;
    }
    if (Date.now() > deadline) {
      throw new Error(`等待本地服务超时：${baseURL}\n${logs()}`);
    }
    await new Promise((resolve) => setTimeout(resolve, 120));
  }
}

async function stopChild(child: ChildProcess): Promise<void> {
  if (child.exitCode !== null) {
    return;
  }
  const exited = new Promise<void>((resolve) => {
    child.once("exit", () => resolve());
  });
  child.kill("SIGTERM");
  const timeout = new Promise<void>((resolve) => {
    setTimeout(() => {
      if (child.exitCode === null) {
        child.kill("SIGKILL");
      }
      resolve();
    }, 5_000);
  });
  await Promise.race([exited, timeout]);
}

export async function startApp(): Promise<AppHandle> {
  const external = process.env.PLAYWRIGHT_BASE_URL?.trim();
  if (external) {
    const baseURL = external.replace(/\/+$/, "");
    const dataDir = process.env.TENGHSHOU_DATA_DIR?.trim() || null;
    const token =
      process.env.TENGHSHOU_API_TOKEN?.trim() ||
      (dataDir ? await readToken(dataDir) : "");
    await waitUntilReady(baseURL, null, () => "（外部服务）");
    return {
      baseURL,
      token,
      dataDir,
      cli: locateCLI(),
      stop: async () => undefined,
    };
  }

  const cli = locateCLI();
  const dataDir = await mkdtemp(join(tmpdir(), "tengshou-e2e-"));
  const port = await freePort();
  const baseURL = `http://127.0.0.1:${port}`;
  const child = spawn(
    cli,
    ["serve", "--data-dir", dataDir, "--port", String(port), "--loopback"],
    { stdio: ["ignore", "pipe", "pipe"], windowsHide: true },
  );
  let output = "";
  child.stdout?.setEncoding("utf8");
  child.stderr?.setEncoding("utf8");
  child.stdout?.on("data", (chunk: string) => {
    output += chunk;
  });
  child.stderr?.on("data", (chunk: string) => {
    output += chunk;
  });
  await waitUntilReady(baseURL, child, () => output);
  const token = await readToken(dataDir);
  return {
    baseURL,
    token,
    dataDir,
    cli,
    stop: async () => {
      await stopChild(child);
      if (process.env.E2E_KEEP_DATA !== "1") {
        await rm(dataDir, { recursive: true, force: true });
      }
    },
  };
}
