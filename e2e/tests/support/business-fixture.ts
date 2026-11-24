// 真实业务验收场景专用的 app fixture：服务以局域网模式启动
// （不带 --loopback，config 默认 allow_lan=true），扫码签名才可能创建会话。
// 独立文件避免影响现有两个场景的 loopback 语义；测试仍以 127.0.0.1 访问。
import { test as base, expect } from "@playwright/test";
import { startApp, type AppHandle } from "./app";

export const test = base.extend<
  Record<string, never>,
  { app: AppHandle }
>({
  app: [
    async ({}, use) => {
      const app = await startApp({ allowLan: true });
      try {
        await use(app);
      } finally {
        await app.stop();
      }
    },
    { scope: "worker" },
  ],
});

export { expect };
