// 每个测试 worker 一个发布形态服务；同一文件内的用例共享该服务与数据目录。
// 不同文件各自拉起独立服务（空闲端口 + 独立临时目录），可并行、互不干扰。
import { test as base, expect } from "@playwright/test";
import { startApp, type AppHandle } from "./app";

export const test = base.extend<Record<string, never>, { app: AppHandle }>({
  app: [
    async ({}, use) => {
      const app = await startApp();
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
