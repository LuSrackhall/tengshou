// 业务验收「打印自助录入海报，租户手机扫码建档」步骤实现（te-48hkl）。
//
// 拆到独立文件，避免 business-acceptance.spec.ts 超过 600 行结构预算。
// 返回一段人类可读的「实际结果」，由验收报告写入对应步骤。
import { readFile } from "node:fs/promises";
import type { BrowserContext, Page } from "@playwright/test";
import { expect } from "@playwright/test";
import type { FrameRecorder } from "./business-frames";

const ONE_PIXEL_PNG = Buffer.from(
  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNkYAAAAAYAAjCB0C8AAAAASUVORK5CYII=",
  "base64",
);

/**
 * 打开相对方档案页的「自助录入海报」，让手机侧扫码完成一次企业租户建档
 * （证件照 + 签名章 + 公章，不手写），并核对桌面收到档案与图片来源。
 */
export async function runPosterEnrollStep(
  page: Page,
  context: BrowserContext,
  frames: FrameRecorder,
  baseURL: string,
  sealPath: string,
): Promise<string> {
  const selfName = "杭州恒信物业服务有限公司";
  const selfID = "91330100MA2SELF001";
  await page.locator('#tabs button[data-tab="parties"]').click();
  await expect(page.locator("#view-parties")).toHaveClass(/active/);
  await page.locator("#enrollPosterBtn").click();
  const poster = page.getByRole("dialog").filter({
    has: page.getByRole("heading", { name: "自助录入海报（可打印）" }),
  });
  await expect(poster).toBeVisible();
  await expect(poster.locator(".poster-url code")).toContainText("/enroll/", {
    timeout: 15_000,
  });
  const url = (await poster.locator(".poster-url code").textContent())?.trim() ?? "";
  const token = url.split("/enroll/")[1] ?? "";
  expect(token).toHaveLength(64);
  await frames.capture("① 档案页的「自助录入海报」：长期二维码 + 打印按钮", {
    region: poster,
  });

  const phone = await context.newPage();
  const response = await phone.goto(`${baseURL}/enroll/${token}`, {
    waitUntil: "domcontentloaded",
  });
  expect(response?.status()).toBe(200);
  await expect(phone.locator("#enrollCard")).toBeVisible();
  // 企业租户选择「单位」类型（te-i11we）：名称标签切换为「单位名称」。
  await phone.locator("#enrollKind").selectOption("organization");
  await expect(phone.locator("#enrollNameLabel")).toHaveText("单位名称");
  await phone.locator("#enrollName").fill(selfName);
  await phone.locator("#enrollIDNumber").fill(selfID);
  await phone.locator("#enrollPhone").fill("13800009999");
  await phone.locator("#enrollIdFront").setInputFiles({
    name: "front.png", mimeType: "image/png", buffer: ONE_PIXEL_PNG,
  });
  await phone.locator("#enrollIdBack").setInputFiles({
    name: "back.png", mimeType: "image/png", buffer: ONE_PIXEL_PNG,
  });
  const sealImage = await readFile(sealPath);
  await phone.locator("#enrollSignatureSeal").setInputFiles({
    name: "signature-seal.png", mimeType: "image/png", buffer: sealImage,
  });
  await phone.locator("#enrollSeal").setInputFiles({
    name: "seal.png", mimeType: "image/png", buffer: sealImage,
  });
  await expect(phone.locator("#enrollSignatureSealPreview")).toBeVisible();
  await expect(phone.locator("#enrollSealPreview")).toBeVisible();
  await frames.capture("② 手机侧自助建档页：证件照、签名章与公章都可上传", {
    region: phone.locator("#enrollCard"),
    page: phone,
  });
  await expect(phone.locator("#enrollSubmitBtn")).toBeEnabled();
  await phone.locator("#enrollSubmitBtn").click();
  await expect(phone.getByRole("heading", { name: "档案已提交" })).toBeVisible({
    timeout: 20_000,
  });
  await frames.capture("③ 手机侧提交成功：档案已保存到出租方电脑本机", {
    region: phone.locator("#enrollCard"),
    page: phone,
  });
  await phone.close();

  await poster.locator(".modal-head button.close").click();
  await expect(poster).toBeHidden();
  const row = page.locator(`#partyTable tr[data-id]:has-text("${selfName}")`);
  await expect(row).toBeVisible({ timeout: 20_000 });
  await expect(row).toContainText("单位");
  const partyId = (await row.getAttribute("data-id")) ?? "";
  expect(partyId).not.toBe("");
  const detail = (await (
    await page.request.get(`${baseURL}/app/api/v1/parties/${partyId}`)
  ).json()) as {
    kind?: string;
    signature_seal_id?: string;
    signature_seal_source?: string;
    seal_asset_id?: string;
    seal_source?: string;
  };
  expect(detail.kind).toBe("organization");
  expect(detail.signature_seal_id ?? "").not.toBe("");
  expect(detail.seal_asset_id ?? "").not.toBe("");
  expect(detail.signature_seal_source).toBe("lan_enroll");
  expect(detail.seal_source).toBe("lan_enroll");
  await frames.capture("④ 桌面收到自助档案：签名章与公章来源为手机自助录入", {
    region: page.locator("#view-parties"),
    targets: [row],
  });
  return (
    `海报入口为长期有效二维码（可打印贴墙）；租户「${selfName}」用手机扫码选择「单位」类型，` +
    "上传证件照、签名章与公章后提交，档案以单位类型自动出现在桌面列表；" +
    "两张图片的来源均记录为 lan_enroll（手机自助录入）。"
  );
}
