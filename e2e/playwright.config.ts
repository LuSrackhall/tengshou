import { defineConfig, devices } from "@playwright/test";

// 三端浏览器选择：macOS 用 WebKit（近似 WKWebView），Linux/Windows 用
// Chromium（Windows 近似 WebView2）。本地调试可用 E2E_BROWSER 覆盖。
type BrowserName = "chromium" | "webkit";

const browserName: BrowserName =
  (process.env.E2E_BROWSER as BrowserName | undefined) ??
  (process.platform === "darwin" ? "webkit" : "chromium");

const device =
  browserName === "webkit" ? devices["Desktop Safari"] : devices["Desktop Chrome"];

export default defineConfig({
  testDir: "./tests",
  // 两个场景各自拉起独立服务与临时数据目录，整套应远低于 10 分钟；
  // 单测超时留足 PDF 生成余量。
  timeout: 180_000,
  expect: { timeout: 15_000 },
  fullyParallel: true,
  forbidOnly: Boolean(process.env.CI),
  retries: process.env.CI ? 1 : 0,
  workers: 2,
  reporter: [
    ["list"],
    // HTML 报告完全离线（不引用 CDN），直接作为 Actions 工件上传。
    ["html", { outputFolder: "playwright-report", open: "never" }],
    // 业务验收报告：测试结束后把过程 WebM 复制到报告目录并补全播放器。
    [
      "./tests/support/report-video-reporter.ts",
      { outputDir: "test-results/business-acceptance" },
    ],
  ],
  use: {
    ...device,
    viewport: { width: 1440, height: 960 },
    locale: "zh-CN",
    acceptDownloads: true,
    // 定位失败时在 30 秒内报错（而不是耗到整条用例超时），失败信息直接
    // 指出缺失的业务文案或选择器。
    actionTimeout: 30_000,
    navigationTimeout: 30_000,
    // 可读工件：每个用例录视频，失败时保留 trace，关键步骤另行截图 attach。
    video: "on",
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  projects: [{ name: browserName, use: { browserName } }],
});
