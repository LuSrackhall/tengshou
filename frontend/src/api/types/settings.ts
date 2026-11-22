// 运行时设置与数据安全契约：MCP 连接器状态、备份恢复、一致性检查。
//
// 来源（与 Go 侧逐字段核对）：
//   internal/httpapi/appapi.go   MCP status / settings 响应；mcpSettingsInput
//   internal/service/backup.go   PendingBackup / BackupStatus / AssetIssue /
//                                IntegrityReport
//   internal/backup/backup.go    Manifest / FileEntry
//   internal/httpapi/backupapi.go 恢复响应包装
//   internal/mcpspec             只读/写工具数量与 actor 名称

import type { ISODateTime } from "./common";

// ---------- MCP 连接器 ----------

/** 最近一次 agent 写入的摘要。 */
export interface MCPLastAction {
  summary: string;
  entity: string;
  entity_id: string;
  action: string;
  created_at: ISODateTime;
}

/** GET /mcp/status 的完整状态（只读，启停连接器由用户自行完成）。 */
export interface MCPStatus {
  version: string;
  write_enabled: boolean;
  read_enabled: boolean;
  read_tools: number;
  write_tools: number;
  /** MCP 写入在变更记录里的 actor 名。 */
  agent_actor: string;
  agent_actions_total: number;
  agent_actions_recent: number;
  last_agent_action: MCPLastAction | null;
  data_dir: string;
  config_file: string;
  transport: string;
  command: string;
  settings_key: string;
  write_switch_applies: string;
  connection_help: string;
}

/** PUT/POST /settings/mcp 的请求体。 */
export interface MCPSettingsInput {
  write_enabled: boolean;
}

/** 设置写入后的响应。 */
export interface MCPSettingsResponse {
  write_enabled: boolean;
}

// ---------- 备份与恢复 ----------

/** 已暂存、等待重启生效的恢复包（service.PendingBackup）。 */
export interface PendingBackup {
  id: string;
  staged_at: ISODateTime;
  created_at: ISODateTime;
  app_version: string;
  schema_version: number;
  file_count: number;
  total_size: number;
}

/** GET /backup/status。 */
export interface BackupStatus {
  configured: boolean;
  data_dir: string;
  pending?: PendingBackup;
}

/** 备份包里的一个载荷文件（backup.FileEntry）。 */
export interface BackupFileEntry {
  path: string;
  size: number;
  sha256: string;
}

/** 备份清单（backup.Manifest）；恢复接口的 item 即该结构。 */
export interface BackupManifest {
  format: string;
  format_version: number;
  id: string;
  app_version: string;
  schema_version: number;
  created_at: ISODateTime;
  platform: string;
  database: string;
  config?: string;
  files: BackupFileEntry[];
}

/** POST /backup/restore（multipart，字段名 file）的响应。 */
export interface BackupRestoreResponse {
  item: BackupManifest;
  restart_required: boolean;
  message: string;
}

// ---------- 一致性检查 ----------

/** 数据库引用了、磁盘上缺失的文件。 */
export interface AssetIssue {
  path: string;
}

/** GET /integrity 的只读报告；不会删除任何文件。 */
export interface IntegrityReport {
  checked_at: ISODateTime;
  database_ok: boolean;
  database_error?: string;
  checked_files: number;
  missing_files: AssetIssue[];
  orphan_files: string[];
}
