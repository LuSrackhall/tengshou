// 模板域的展示与文件工具：逐条对应存量 app.js 的同名逻辑。
import type { Placement, Template } from "../../api/types";

/** 与 app.js fmtDate 一致的日期格式（模板页沿用 formatDateTime 名称）。 */
export { fmtDate as formatDateTime } from "../../ui/format";

/** 模板转换方式文案；与 app.js templateConverterLabel 逐字一致。 */
export function converterLabel(converter?: string): string {
  if (converter === "word") {
    return "Microsoft Word";
  }
  if (converter === "libreoffice") {
    return "LibreOffice";
  }
  if (converter === "pdf") {
    return "PDF 直接导入";
  }
  return converter || "—";
}

/** FileReader → base64（去掉 data: 前缀）；错误文案与 app.js 一致。 */
export function readFileBase64(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => {
      const result = String(reader.result || "");
      const index = result.indexOf(",");
      resolve(index >= 0 ? result.slice(index + 1) : result);
    };
    reader.onerror = () => reject(new Error("读取文件失败"));
    reader.readAsDataURL(file);
  });
}

/** base64 → 字节；与 app.js base64ToBytes 一致。 */
export function base64ToBytes(base64: string): Uint8Array {
  const binary = atob(base64);
  const out = new Uint8Array(binary.length);
  for (let index = 0; index < binary.length; index += 1) {
    out[index] = binary.charCodeAt(index);
  }
  return out;
}

/** 以指定文件名保存 Blob（模板域本地工具，导出口径与本域下载命名一致）。 */
export function saveBlob(blob: Blob, filename: string): void {
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = filename;
  document.body.appendChild(link);
  link.click();
  link.remove();
  setTimeout(() => URL.revokeObjectURL(url), 2000);
}

/** 下载内存中的字节（样例试生成 PDF）。 */
export function downloadBytes(bytes: Uint8Array, filename: string): void {
  // 复制到独立 ArrayBuffer：TS 的 BlobPart 不接受带 SharedArrayBuffer 联合的视图。
  const copy = new ArrayBuffer(bytes.byteLength);
  new Uint8Array(copy).set(bytes);
  saveBlob(new Blob([copy], { type: "application/pdf" }), filename);
}

/** 新标定位置 id；与 app.js newPlacementID 的前缀与随机段一致。 */
export function newPlacementId(): string {
  return `p_${Date.now().toString(36)}${Math.random().toString(36).slice(2, 7)}`;
}

/** 坐标保留 1 位小数（PDF 点）。 */
export function roundPt(value: number): number {
  return Math.round(value * 10) / 10;
}

export function clampNumber(value: number, min: number, limit: number): number {
  let max = limit;
  if (max < min) {
    max = min;
  }
  if (value < min) {
    return min;
  }
  if (value > max) {
    return max;
  }
  return value;
}

/** 原文件下载名：${name}-v${version}${ext}（扩展名缺失时按 .docx）。 */
export function templateSourceName(template: Template): string {
  const match = String(template.source_path || "").match(/\.[a-z0-9]+$/i);
  const ext = match ? match[0] : ".docx";
  return `${template.name}-v${template.version}${ext}`;
}

/** 深拷贝标定位置（只含基本字段，等价迁移前的 JSON 往返）。 */
export function clonePlacements(placements?: Placement[]): Placement[] {
  return (placements ?? []).map((placement) => ({ ...placement }));
}
