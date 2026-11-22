// 下载类响应的落盘（与 app.js downloadWorkbook 的浏览器行为一致）：
// 生成 blob URL、a[download] 点击，随后回收 URL。

import type { DownloadedFile } from "../../api/client";

// forceFallback=true 时忽略服务端 Content-Disposition 文件名，始终用固定名落盘
// （与 app.js downloadAsset 的迁移前行为一致；生成记录下载用内部合同 ID 命名
// 会改变用户可见文件名，故面板传 true）。
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
