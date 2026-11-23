// 腾手 App API 客户端：统一 base、JSON、错误信封、query 拼装、超时与断连归类。
//
// 约定：
//   - 所有 app API 路径都相对 /app/api/v1，调用方传 "/parties" 这样的路径；
//   - 写操作自动带 X-Tengshou-App: 1（服务端 withAppGuard 的要求）；
//   - 错误统一抛 ApiError：code 优先取服务端 error.code，网络层失败归为
//     network / timeout / invalid_response；
//   - 调用方主动取消（传入的 AbortSignal 触发）时原样抛出 AbortError，
//     不包装成 ApiError，便于区分「取消」与「失败」；
//   - 禁止在组件里裸 fetch：下载、上传、SSE 地址也走这里的 helper。

import type { ApiErrorBody, ApiFieldError } from "./types";

/** app API 的固定 base；与 internal/httpapi/server.go 的 routes() 一致。 */
export const API_BASE = "/app/api/v1";

/** 请求级选项：query 由 buildQuery 统一编码，headers 可覆盖默认值。 */
export interface RequestOptions {
  query?: object;
  signal?: AbortSignal;
  /** 毫秒；<=0 或缺省表示不设超时（生成/导入等长任务不要设）。 */
  timeoutMs?: number;
  headers?: Record<string, string>;
}

/** 客户端侧（非服务端）错误码。 */
export const ApiClientErrorCode = {
  Network: "network",
  Timeout: "timeout",
  InvalidResponse: "invalid_response",
  Http: "http_error",
} as const;

export type ApiClientErrorCodeValue =
  (typeof ApiClientErrorCode)[keyof typeof ApiClientErrorCode];

export interface ApiErrorInit {
  message: string;
  code: string;
  /** HTTP 状态码；网络层错误为 0。 */
  status: number;
  fields?: ApiFieldError[];
  /** 原始响应体（JSON 解析成功时为对象，否则为文本）。 */
  payload?: unknown;
}

/** 所有 app API 失败的统一错误类型。 */
export class ApiError extends Error {
  readonly code: string;
  readonly status: number;
  readonly fields: ApiFieldError[];
  readonly payload: unknown;

  constructor(init: ApiErrorInit) {
    super(init.message);
    this.name = "ApiError";
    this.code = init.code;
    this.status = init.status;
    this.fields = init.fields ?? [];
    this.payload = init.payload;
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

/** 判断响应体是否为 {"error":{code,message,...}} 信封。 */
export function isApiErrorBody(value: unknown): value is ApiErrorBody {
  if (!isRecord(value) || !isRecord(value.error)) {
    return false;
  }
  return (
    typeof value.error.code === "string" &&
    typeof value.error.message === "string"
  );
}

function isAbortError(error: unknown): boolean {
  return isRecord(error) && error.name === "AbortError";
}

/**
 * 拼 query 串；与存量 app.js 的 URLSearchParams 行为一致。
 * 只编码字符串与数字；undefined / null / 空串会被省略。
 * 布尔值不自动转换（archived 等参数请显式传 "1"）。
 */
export function buildQuery(query?: object): string {
  if (!query) {
    return "";
  }
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(query)) {
    if (typeof value !== "string" && typeof value !== "number") {
      continue;
    }
    if (value === "") {
      continue;
    }
    params.set(key, String(value));
  }
  const encoded = params.toString();
  return encoded === "" ? "" : `?${encoded}`;
}

/** 路径段编码；与 app.js 的 encodeURIComponent 一致。 */
export function encodePathSegment(value: string): string {
  return encodeURIComponent(value);
}

/** 生成可直接用于 <a href> / <img src> / pdf.js 的 API 地址。 */
export function apiUrl(path: string, query?: object): string {
  if (!path.startsWith("/")) {
    throw new Error(`API 路径必须以 / 开头：${path}`);
  }
  return `${API_BASE}${path}${buildQuery(query)}`;
}

function defaultHeaders(extra?: Record<string, string>): Record<string, string> {
  return { "X-Tengshou-App": "1", ...extra };
}

/**
 * 发起请求并处理取消/超时/断连。
 * 调用方自己的 abort 原样抛出；超时与网络失败包装成 ApiError。
 */
async function send(
  path: string,
  init: RequestInit,
  options: RequestOptions,
): Promise<Response> {
  const controller = new AbortController();
  const external = options.signal;
  let timedOut = false;
  let timer: ReturnType<typeof setTimeout> | undefined;

  const abort = (): void => controller.abort();
  if (external) {
    if (external.aborted) {
      abort();
    } else {
      external.addEventListener("abort", abort, { once: true });
    }
  }
  if (options.timeoutMs !== undefined && options.timeoutMs > 0) {
    timer = setTimeout(() => {
      timedOut = true;
      abort();
    }, options.timeoutMs);
  }

  try {
    return await fetch(apiUrl(path, options.query), {
      ...init,
      signal: controller.signal,
    });
  } catch (error) {
    if (timedOut) {
      throw new ApiError({
        code: ApiClientErrorCode.Timeout,
        message: "请求超时，本地服务可能无响应，请稍后重试",
        status: 0,
        payload: error,
      });
    }
    if (external?.aborted === true || isAbortError(error)) {
      throw error;
    }
    throw new ApiError({
      code: ApiClientErrorCode.Network,
      message: "无法连接本地服务，请确认腾手仍在运行",
      status: 0,
      payload: error,
    });
  } finally {
    if (timer !== undefined) {
      clearTimeout(timer);
    }
    external?.removeEventListener("abort", abort);
  }
}

async function readBody(response: Response): Promise<unknown> {
  const raw = await response.text().catch(() => "");
  if (raw === "") {
    return null;
  }
  try {
    const parsed: unknown = JSON.parse(raw);
    return parsed;
  } catch {
    return raw;
  }
}

async function errorFromResponse(response: Response): Promise<ApiError> {
  const data = await readBody(response);
  const body = isApiErrorBody(data) ? data : null;
  return new ApiError({
    message: body?.error.message ?? `请求失败（${response.status}）`,
    code: body?.error.code ?? ApiClientErrorCode.Http,
    status: response.status,
    fields: body?.error.fields ?? [],
    payload: data,
  });
}

async function parseJSON<T>(response: Response): Promise<T> {
  const raw = await response.text();
  if (raw === "") {
    return undefined as T;
  }
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    throw new ApiError({
      code: ApiClientErrorCode.InvalidResponse,
      message: "本地服务返回了无法解析的响应",
      status: response.status,
      payload: raw,
    });
  }
  return parsed as T;
}

async function requestJSON<T>(
  method: string,
  path: string,
  body: unknown,
  options: RequestOptions,
): Promise<T> {
  const headers = defaultHeaders(options.headers);
  let payload: string | undefined;
  if (body !== undefined) {
    headers["Content-Type"] = "application/json";
    payload = JSON.stringify(body);
  }
  const response = await send(
    path,
    { method, headers, body: payload },
    options,
  );
  if (!response.ok) {
    throw await errorFromResponse(response);
  }
  if (response.status === 204) {
    return undefined as T;
  }
  return parseJSON<T>(response);
}

/** 下载类响应：原始 Blob + 服务端建议的文件名。 */
export interface DownloadedFile {
  blob: Blob;
  filename: string | null;
}

function filenameFromDisposition(value: string | null): string | null {
  if (!value) {
    return null;
  }
  const star = /filename\*=UTF-8''([^;]+)/i.exec(value);
  const plain = /filename="([^"]*)"/i.exec(value);
  const raw = star?.[1] ?? plain?.[1];
  if (raw === undefined || raw === "") {
    return null;
  }
  try {
    return decodeURIComponent(raw);
  } catch {
    return raw;
  }
}

async function requestFile(
  method: string,
  path: string,
  body: unknown,
  options: RequestOptions,
): Promise<DownloadedFile> {
  const headers = defaultHeaders(options.headers);
  let payload: string | undefined;
  if (body !== undefined) {
    headers["Content-Type"] = "application/json";
    payload = JSON.stringify(body);
  }
  const response = await send(
    path,
    { method, headers, body: payload },
    options,
  );
  if (!response.ok) {
    throw await errorFromResponse(response);
  }
  return {
    blob: await response.blob(),
    filename: filenameFromDisposition(
      response.headers.get("content-disposition"),
    ),
  };
}

// ---------- 导出的 typed helper ----------

export function get<T>(path: string, options: RequestOptions = {}): Promise<T> {
  return requestJSON<T>("GET", path, undefined, options);
}

export function post<T>(
  path: string,
  body?: unknown,
  options: RequestOptions = {},
): Promise<T> {
  return requestJSON<T>("POST", path, body, options);
}

export function put<T>(
  path: string,
  body?: unknown,
  options: RequestOptions = {},
): Promise<T> {
  return requestJSON<T>("PUT", path, body, options);
}

export function del<T>(
  path: string,
  options: RequestOptions = {},
): Promise<T> {
  return requestJSON<T>("DELETE", path, undefined, options);
}

/** GET 一个二进制文件（Excel、PDF、图片、备份包）。 */
export function getFile(
  path: string,
  options: RequestOptions = {},
): Promise<DownloadedFile> {
  return requestFile("GET", path, undefined, options);
}

/** POST 一个返回二进制文件的接口（如签名图片导出）。 */
export function postFile(
  path: string,
  body?: unknown,
  options: RequestOptions = {},
): Promise<DownloadedFile> {
  return requestFile("POST", path, body, options);
}

/**
 * multipart/form-data 上传（备份恢复）。不设置 Content-Type，
 * 交给浏览器补充 boundary。
 */
export async function postForm<T>(
  path: string,
  form: FormData,
  options: RequestOptions = {},
): Promise<T> {
  const response = await send(
    path,
    { method: "POST", headers: defaultHeaders(options.headers), body: form },
    options,
  );
  if (!response.ok) {
    throw await errorFromResponse(response);
  }
  return parseJSON<T>(response);
}
