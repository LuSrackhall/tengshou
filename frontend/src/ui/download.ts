// 下载类响应的落盘（与 app.js downloadBlob 的浏览器行为一致）：
// 生成 blob URL、a[download] 点击，随后回收 URL。
//
// 合同台账、模板、签名导出与数据安全备份共用这一份实现。

import type { DownloadedFile } from "../api/client";

export function saveDownloadedFile(
  file: DownloadedFile,
  fallbackName: string,
  forceFallback = false,
): void {
  const filename = forceFallback ? fallbackName : file.filename || fallbackName;
  const url = URL.createObjectURL(file.blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = filename;
  document.body.appendChild(link);
  link.click();
  link.remove();
  setTimeout(() => URL.revokeObjectURL(url), 2000);
}
