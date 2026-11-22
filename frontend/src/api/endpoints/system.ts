// 运行时设置与数据安全端点。
//
//   GET  /mcp/status、POST /settings/mcp
//   GET  /backup/export、/backup/status、POST /backup/restore
//   GET  /integrity

import {
  apiUrl,
  get,
  getFile,
  post,
  postForm,
  type DownloadedFile,
  type RequestOptions,
} from "../client";
import type {
  BackupRestoreResponse,
  BackupStatus,
  IntegrityReport,
  MCPSettingsInput,
  MCPSettingsResponse,
  MCPStatus,
} from "../types";

/** GET /mcp/status：连接器只读状态与最近 agent 动作。 */
export function getMCPStatus(options: RequestOptions = {}): Promise<MCPStatus> {
  return get("/mcp/status", options);
}

/** POST /settings/mcp：切换 MCP 写开关（仅本机界面可调用）。 */
export function updateMCPSettings(
  input: MCPSettingsInput,
  options: RequestOptions = {},
): Promise<MCPSettingsResponse> {
  return post("/settings/mcp", input, options);
}

/** GET /backup/export：完整备份包的下载地址。 */
export function backupExportUrl(): string {
  return apiUrl("/backup/export");
}

/** 下载完整备份包（zip）。 */
export function downloadBackup(
  options: RequestOptions = {},
): Promise<DownloadedFile> {
  return getFile("/backup/export", options);
}

/** GET /backup/status：数据目录与待重启生效的恢复包。 */
export function getBackupStatus(
  options: RequestOptions = {},
): Promise<BackupStatus> {
  return get("/backup/status", options);
}

/** POST /backup/restore：上传 zip 并暂存，重启后生效。 */
export function restoreBackup(
  file: Blob,
  filename: string,
  options: RequestOptions = {},
): Promise<BackupRestoreResponse> {
  const form = new FormData();
  form.append("file", file, filename);
  return postForm("/backup/restore", form, options);
}

/** GET /integrity：数据库与资产文件的一致性报告（只读）。 */
export function getIntegrityReport(
  options: RequestOptions = {},
): Promise<IntegrityReport> {
  return get("/integrity", options);
}
