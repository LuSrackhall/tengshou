// 场景 D：租户档案升级与局域网自助建档（v0.2）。
//
// 业务含义：出租方在「相对方档案」页生成一次性二维码，租户用手机（测试用
// 本机回环模拟手机浏览器）填写姓名、证件号、上传证件照正反面并手写签名；
// 提交后档案自动建立，出租方档案列表通过 SSE/轮询刷新看到新档案。
// 全程只驱动发布形态的 tengshou-cli serve，不访问公网。
import { expect, test } from "./support/business-fixture";
import { shot, trackConsoleErrors } from "./support/ui";

// 1x1 PNG：服务端会把证件照压缩为 JPEG；尺寸只要求可解码。
const ONE_PIXEL_PNG = Buffer.from(
  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNkYAAAAAYAAjCB0C8AAAAASUVORK5CYII=",
  "base64",
);

test.describe("场景 D：局域网自助建档", () => {
  test("生成二维码 → 手机填写档案 → 提交后档案列表自动出现", async ({
    app,
    page,
    context,
  }, testInfo) => {
    const consoleErrors = trackConsoleErrors(page);
    const stamp = Date.now().toString(36);
    const tenantName = `自助租户 ${stamp}`;
    const idNumber = `33010219800101${stamp.slice(-6).padEnd(6, "0")}`;

    await test.step("打开发布形态界面并进入相对方档案", async () => {
      const response = await page.goto(`${app.baseURL}/app/`, {
        waitUntil: "domcontentloaded",
      });
      expect(response?.status()).toBe(200);
      await page.locator('#tabs button[data-tab="parties"]').click();
      await expect(page.locator("#view-parties")).toHaveClass(/active/);
      await shot(page, testInfo, "01-相对方档案页");
    });

    let enrollURL = "";
    let enrollDialog = page.getByRole("dialog").filter({
      has: page.getByRole("heading", { name: "租户自助建档二维码" }),
    });

    await test.step("出租方生成自助建档二维码", async () => {
      await page.locator("#enrollQrBtn").click();
      enrollDialog = page.getByRole("dialog").filter({
        has: page.getByRole("heading", { name: "租户自助建档二维码" }),
      });
      await expect(enrollDialog).toBeVisible();
      await expect(enrollDialog.locator("#enrollQrImg")).toBeVisible({
        timeout: 15_000,
      });
      const urlText =
        (await enrollDialog.locator(".sign-url").textContent())?.trim() ?? "";
      expect(urlText).toContain("/enroll/");
      const token = urlText.split("/enroll/")[1] ?? "";
      expect(token).toHaveLength(64);
      // 本机回环模拟手机浏览器：仍走同一个 LAN-only、一次性令牌页面。
      enrollURL = `${app.baseURL}/enroll/${token}`;
      await shot(page, testInfo, "02-自助建档二维码已生成");
    });

    const phone = await context.newPage();
    const phoneErrors = trackConsoleErrors(phone);
    await test.step("手机页填写姓名、证件号并上传证件照", async () => {
      const response = await phone.goto(enrollURL, {
        waitUntil: "domcontentloaded",
      });
      expect(response?.status()).toBe(200);
      await expect(phone.locator("#enrollCard")).toBeVisible();
      await phone.locator("#enrollName").fill(tenantName);
      await phone.locator("#enrollIDNumber").fill(idNumber);
      await phone.locator("#enrollPhone").fill("13800001111");
      await phone
        .locator("#enrollIdFront")
        .setInputFiles({ name: "front.png", mimeType: "image/png", buffer: ONE_PIXEL_PNG });
      await phone
        .locator("#enrollIdBack")
        .setInputFiles({ name: "back.png", mimeType: "image/png", buffer: ONE_PIXEL_PNG });
      await expect(phone.locator("#enrollIdFrontPreview")).toBeVisible();
      await expect(phone.locator("#enrollIdBackPreview")).toBeVisible();
    });

    await test.step("手机页手写签名并提交", async () => {
      const canvas = phone.locator("#enrollPad");
      await canvas.scrollIntoViewIfNeeded();
      const box = await canvas.boundingBox();
      expect(box).not.toBeNull();
      if (!box) {
        return;
      }
      const startX = box.x + 40;
      const endX = box.x + box.width - 40;
      const midY = box.y + box.height / 2;
      await phone.mouse.move(startX, midY);
      await phone.mouse.down();
      for (let i = 1; i <= 24; i += 1) {
        const x = startX + ((endX - startX) * i) / 24;
        const y = midY + Math.sin(i / 1.7) * (box.height / 5);
        await phone.mouse.move(x, y, { steps: 3 });
      }
      await phone.mouse.up();
      await expect(phone.locator("#enrollSubmitBtn")).toBeEnabled();
      await phone.locator("#enrollSubmitBtn").click();
      await expect(
        phone.getByRole("heading", { name: "档案已提交" }),
      ).toBeVisible({ timeout: 20_000 });
      await shot(phone, testInfo, "03-手机提交成功");
    });

    await test.step("出租方档案列表自动出现新档案", async () => {
      // 手机提交后桌面端轮询会让页脚「取消」翻成「关闭」，与弹窗头部 × 的
      // aria-label「关闭」重名，按角色名点击会 strict mode 命中两个元素
      // （Linux Chromium 轮询更快，连续复现）；用头部 × 精确定位。
      await enrollDialog.locator(".modal-head button.close").click();
      await expect(enrollDialog).toBeHidden();
      const row = page.locator(
        `#partyTable tr[data-id]:has-text("${tenantName}")`,
      );
      await expect(row).toBeVisible({ timeout: 20_000 });
      await expect(row).toContainText(idNumber);
      await shot(page, testInfo, "04-自助建档已进入档案列表");
    });

    await test.step("档案页可查看证件照与档案签名", async () => {
      const row = page.locator(
        `#partyTable tr[data-id]:has-text("${tenantName}")`,
      );
      await row.click();
      const archiveDialog = page.getByRole("dialog").filter({
        has: page.getByRole("heading", { name: `编辑相对方 · ${tenantName}` }),
      });
      await expect(archiveDialog).toBeVisible();
      await expect(archiveDialog.getByRole("heading", { name: "证件、签名与公章" })).toBeVisible();
      await expect(archiveDialog.locator('img[alt="证件照正面"]')).toBeVisible();
      await expect(archiveDialog.locator('img[alt="证件照反面"]')).toBeVisible();
      await expect(archiveDialog.locator('img[alt="档案签名"]')).toBeVisible();
      await shot(page, testInfo, "05-档案详情中的证件照与签名");
    });

    const archiveDialog = page.getByRole("dialog").filter({
      has: page.getByRole("heading", { name: `编辑相对方 · ${tenantName}` }),
    });

    await test.step("档案页可更换证件照", async () => {
      await archiveDialog
        .locator('input[aria-label="选择证件照正面"]')
        .setInputFiles({
          name: "front-replaced.png",
          mimeType: "image/png",
          buffer: ONE_PIXEL_PNG,
        });
      await expect(page.locator("#toast")).toContainText("证件照正面已保存");
      await expect(archiveDialog.locator('img[alt="证件照正面"]')).toBeVisible();
      await shot(page, testInfo, "06-证件照已更换");
    });

    await test.step("档案页扫码重新采集签名", async () => {
      await archiveDialog.getByRole("button", { name: "重新采集" }).click();
      const signDialog = page.getByRole("dialog").filter({
        has: page.getByRole("heading", { name: `档案签名 · ${tenantName}` }),
      });
      await expect(signDialog).toBeVisible();
      await expect(signDialog.locator("#partySignQrImg")).toBeVisible({
        timeout: 15_000,
      });
      const signUrlText =
        (await signDialog.locator(".sign-url").textContent())?.trim() ?? "";
      expect(signUrlText).toContain("/sign/");
      const signToken = signUrlText.split("/sign/")[1] ?? "";
      expect(signToken).toHaveLength(64);

      const signPhone = await context.newPage();
      const signPhoneErrors = trackConsoleErrors(signPhone);
      const response = await signPhone.goto(`${app.baseURL}/sign/${signToken}`, {
        waitUntil: "domcontentloaded",
      });
      expect(response?.status()).toBe(200);
      await expect(signPhone.locator("#signCard")).toBeVisible();

      const pad = signPhone.locator("#pad");
      await pad.scrollIntoViewIfNeeded();
      const padBox = await pad.boundingBox();
      expect(padBox).not.toBeNull();
      if (padBox) {
        const startX = padBox.x + 40;
        const endX = padBox.x + padBox.width - 40;
        const midY = padBox.y + padBox.height / 2;
        await signPhone.mouse.move(startX, midY);
        await signPhone.mouse.down();
        for (let i = 1; i <= 24; i += 1) {
          const x = startX + ((endX - startX) * i) / 24;
          const y = midY + Math.sin(i / 1.7) * (padBox.height / 5);
          await signPhone.mouse.move(x, y, { steps: 3 });
        }
        await signPhone.mouse.up();
      }
      await expect(signPhone.locator("#submitBtn")).toBeEnabled();
      await signPhone.locator("#submitBtn").click();
      await expect(
        signPhone.getByRole("heading", { name: "签名已提交" }),
      ).toBeVisible({ timeout: 20_000 });

      // 电脑端 2 秒轮询拿到完成状态后关闭对话框并刷新签名图片。
      await expect(signDialog).toBeHidden({ timeout: 20_000 });
      await expect(page.locator("#toast")).toContainText("档案签名已保存");
      await expect(archiveDialog.locator('img[alt="档案签名"]')).toBeVisible();
      await shot(page, testInfo, "07-档案签名已重新采集");
      expect(signPhoneErrors).toEqual([]);
      await signPhone.close();
    });

    await test.step("关闭档案页", async () => {
      await archiveDialog.getByRole("button", { name: "取消" }).click();
      await expect(archiveDialog).toBeHidden();
    });

    expect(phoneErrors).toEqual([]);
    expect(consoleErrors).toEqual([]);
  });
});
