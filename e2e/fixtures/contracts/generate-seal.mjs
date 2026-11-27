// 生成 E2E 真实业务场景使用的白底红色公章图片（虚构单位）。
//
// 产物：e2e/fixtures/contracts/出租方公章.png（400×400，白底 RGB）。
// 章面文字：幸福村股份经济合作社（上弧）+ 合同专用章（中下）+ 虚构编号（下弧）。
// 白底是刻意的：产品导入公章时会自动去白底并裁剪边距，本夹具用来验证这条
// 真实处理链路。图片内容全部虚构，仅用于界面端到端验收。
//
// 用法（在 e2e 目录已 npm ci、已安装 Playwright Chromium 后）：
//   node fixtures/contracts/generate-seal.mjs
import { chromium } from "@playwright/test";
import { fileURLToPath } from "node:url";

const output = fileURLToPath(new URL("./出租方公章.png", import.meta.url));

const html = `<!doctype html>
<html lang="zh-CN">
<head><meta charset="utf-8"><style>
  html, body { margin: 0; background: #fff; }
  #seal { width: 400px; height: 400px; display: block; }
</style></head>
<body>
<svg id="seal" width="400" height="400" viewBox="0 0 400 400" xmlns="http://www.w3.org/2000/svg">
  <defs>
    <path id="topArc" d="M 55 210 A 145 145 0 0 1 345 210"/>
    <path id="bottomArc" d="M 72 215 A 128 128 0 0 0 328 215"/>
  </defs>
  <rect width="400" height="400" fill="#ffffff"/>
  <circle cx="200" cy="205" r="182" fill="none" stroke="#d40000" stroke-width="9"/>
  <circle cx="200" cy="205" r="168" fill="none" stroke="#d40000" stroke-width="2"/>
  <text font-family="'Noto Sans CJK SC','PingFang SC','Microsoft YaHei',sans-serif"
        font-size="31" font-weight="700" fill="#d40000" letter-spacing="2">
    <textPath href="#topArc" startOffset="50%" text-anchor="middle">幸福村股份经济合作社</textPath>
  </text>
  <path fill="#d40000" d="M200 148 l14.6 44.9h47.2l-38.2 27.8 14.6 44.9-38.2-27.8-38.2 27.8 14.6-44.9-38.2-27.8h47.2z"/>
  <text x="200" y="290" text-anchor="middle"
        font-family="'Noto Sans CJK SC','PingFang SC','Microsoft YaHei',sans-serif"
        font-size="27" font-weight="700" fill="#d40000" letter-spacing="4">合同专用章</text>
  <text font-family="'Noto Sans CJK SC','PingFang SC','Microsoft YaHei',sans-serif"
        font-size="18" fill="#d40000" letter-spacing="1">
    <textPath href="#bottomArc" startOffset="50%" text-anchor="middle">编号 3301008888666（虚构）</textPath>
  </text>
</svg>
</body></html>`;

const browser = await chromium.launch().catch(() =>
  // 本地没有 Playwright 自带 Chromium 时退回系统 Chrome（仅用于重新生成夹具）。
  chromium.launch({ channel: "chrome" }),
);
try {
  const page = await browser.newPage({
    viewport: { width: 400, height: 400 },
    deviceScaleFactor: 1,
  });
  await page.setContent(html, { waitUntil: "load" });
  await page.locator("#seal").screenshot({ path: output, omitBackground: false });
} finally {
  await browser.close();
}
console.log(`已生成：${output}`);
