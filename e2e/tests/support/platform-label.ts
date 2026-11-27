// 平台与浏览器项目的中文展示名（业务验收报告使用）。

/** 报告里用人类可读的平台名，而不是 process.platform 的 node 取值。 */
export function platformLabel(): string {
  switch (process.platform) {
    case "win32":
      return "Windows";
    case "darwin":
      return "macOS";
    case "linux":
      return "Linux";
    default:
      return process.platform;
  }
}

/** 浏览器项目与桌面壳引擎的对应关系（已知近似，报告如实标注）。 */
export function browserLabel(projectName: string): string {
  if (projectName === "webkit") {
    return "WebKit（近似桌面壳 WKWebView）";
  }
  if (platformLabel() === "Windows") {
    return "Chromium（近似桌面壳 WebView2）";
  }
  return "Chromium";
}
