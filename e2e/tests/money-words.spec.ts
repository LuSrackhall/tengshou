// 场景 G：合同金额中文大写（te-7c5v1）。
//
// 业务含义：
//   - 合同表单里每个金额字段实时显示自动大写，可「改」为手填覆盖；
//   - 清空覆盖恢复自动；续签时覆盖值随合同内容带出；
//   - 模板可绑定 contract.amount.words / contract.data.<金额键>.words，
//     生成的 PDF 使用自动大写或手填覆盖。
//
// 只驱动发布形态的 tengshou-cli serve；模板/字段/合同数据经页面接口准备，
// 表单交互与生成结果走界面接口与 PDF 文本断言。
import { readFile } from "node:fs/promises";
import { join } from "node:path";
import { expect, test } from "./support/app-fixture";
import { analyzePdf } from "./support/business-pdf";
import { repoRoot } from "./support/app";
import { shot, trackConsoleErrors } from "./support/ui";
import type { AppHandle } from "./support/app";
import type { Locator, Page } from "@playwright/test";

const APP_HEADERS = { "X-Tengshou-App": "1" };

async function postJSON<T>(
  page: Page,
  app: AppHandle,
  path: string,
  data: unknown,
): Promise<T> {
  const response = await page.request.post(`${app.baseURL}/app/api/v1${path}`, {
    headers: APP_HEADERS,
    data,
  });
  expect(response.ok(), `${path} → ${response.status()}`).toBeTruthy();
  return (await response.json()) as T;
}

async function getJSON<T>(page: Page, app: AppHandle, path: string): Promise<T> {
  const response = await page.request.get(`${app.baseURL}/app/api/v1${path}`);
  expect(response.ok(), `${path} → ${response.status()}`).toBeTruthy();
  return (await response.json()) as T;
}

async function putJSON<T>(
  page: Page,
  app: AppHandle,
  path: string,
  data: unknown,
): Promise<T> {
  const response = await page.request.put(`${app.baseURL}/app/api/v1${path}`, {
    headers: APP_HEADERS,
    data,
  });
  expect(response.ok(), `PUT ${path} → ${response.status()}`).toBeTruthy();
  return (await response.json()) as T;
}

interface ContractItem {
  id: string;
  contract_no: string;
  title: string;
  amount: number;
  data: Record<string, unknown>;
  source_contract_id?: string;
}

async function seedParty(page: Page, app: AppHandle, stamp: string): Promise<string> {
  const payload = await postJSON<{ item: { id: string } }>(page, app, "/parties", {
    kind: "person",
    name: `E2E 大写租户 ${stamp}`,
    id_number: `MW${stamp}`,
  });
  return payload.item.id;
}

async function seedMoneyField(
  page: Page,
  app: AppHandle,
  stamp: string,
): Promise<{ key: string; label: string }> {
  const key = `words_money_${stamp}`;
  const label = `E2E 金额 ${stamp}`;
  await postJSON(page, app, "/fields", {
    entity: "contract",
    key,
    label,
    type: "money",
  });
  return { key, label };
}

async function openNewContract(page: Page): Promise<Locator> {
  await page.locator('#tabs button[data-tab="contracts"]').click();
  await expect(page.locator("#view-contracts")).toHaveClass(/active/);
  await page.locator("#newContractBtn").click();
  const dialog = page
    .getByRole("dialog")
    .filter({ has: page.getByRole("heading", { name: "新建合同" }) });
  await expect(dialog).toBeVisible();
  return dialog;
}

async function choosePartyB(dialog: Locator, name: string): Promise<void> {
  const field = dialog.locator('.picker-field[data-key="party_b"]');
  await field.locator("#f_contract_party_b").fill(name);
  await field
    .locator(`#partyBPickerList button[data-party-id]:has-text("${name}")`)
    .click();
  await expect(field.locator(".picker-selected")).toContainText(name);
}

async function editContract(page: Page, title: string): Promise<Locator> {
  const row = page.locator(`#contractTable tr[data-id]:has-text("${title}")`).last();
  await expect(row).toBeVisible();
  await row.click();
  const dialog = page
    .getByRole("dialog")
    .filter({ has: page.getByRole("heading", { name: /编辑合同/ }) });
  await expect(dialog).toBeVisible();
  return dialog;
}

test.describe("场景 G1：合同表单金额大写", () => {
  test("自动预览随金额变化，手改覆盖可清空，续签保留覆盖值", async ({
    app,
    page,
  }, testInfo) => {
    const consoleErrors = trackConsoleErrors(page);
    const stamp = Date.now().toString(36);
    const tenantName = `E2E 大写租户 ${stamp}`;
    const contractTitle = `E2E 大写合同 ${stamp}`;
    const override = "人民币玖仟玖佰玖拾玖元整（手填）";

    const partyID = await seedParty(page, app, stamp);
    const money = await seedMoneyField(page, app, stamp);

    await page.goto(`${app.baseURL}/app/`, { waitUntil: "domcontentloaded" });
    let contractID = "";
    await test.step("金额输入实时生成大写，动态金额字段同样生效", async () => {
      const dialog = await openNewContract(page);
      await dialog.locator("#f_contract_title").fill(contractTitle);
      await choosePartyB(dialog, tenantName);

      const amountField = dialog.locator('[data-key="amount"]');
      await amountField.locator("#f_contract_amount").fill("1500.50");
      await expect(amountField.locator(".money-words-auto")).toContainText(
        "大写（自动）：人民币壹仟伍佰元伍角",
      );
      await amountField.locator("#f_contract_amount").fill("12500.34");
      await expect(amountField.locator(".money-words-auto")).toContainText(
        "人民币壹万贰仟伍佰元叁角肆分",
      );

      const moneyField = dialog.locator(`[data-key="${money.key}"]`);
      await moneyField.locator(`#f_contract_${money.key}`).fill("1000.05");
      await expect(moneyField.locator(".money-words-auto")).toContainText(
        "人民币壹仟元零伍分",
      );
      await shot(page, testInfo, "01-自动大写随金额变化");
    });

    await test.step("「改」切换手填覆盖并随合同保存", async () => {
      const dialog = page
        .getByRole("dialog")
        .filter({ has: page.getByRole("heading", { name: "新建合同" }) });
      const amountField = dialog.locator('[data-key="amount"]');
      await amountField.getByRole("button", { name: "改", exact: true }).click();
      const wordsInput = amountField.locator("#f_contract_words_amount");
      await expect(wordsInput).toHaveValue("人民币壹万贰仟伍佰元叁角肆分");
      await wordsInput.fill(override);
      await dialog.getByRole("button", { name: "保存", exact: true }).click();
      await expect(dialog).toBeHidden();

      const list = await getJSON<{ items: ContractItem[] }>(
        page,
        app,
        `/contracts?limit=100`,
      );
      const saved = list.items.find((item) => item.title === contractTitle);
      expect(saved).toBeTruthy();
      contractID = saved?.id ?? "";
      expect(saved?.data.__amount_words__).toBe(override);
      // 动态金额字段保持自动：表单提交空串，服务端不落覆盖键。
      expect(saved?.data[`__words__${money.key}`] ?? "").toBe("");
      await shot(page, testInfo, "02-手填覆盖已保存");
    });

    await test.step("编辑时回显覆盖；清空覆盖恢复自动", async () => {
      const dialog = await editContract(page, contractTitle);
      const amountField = dialog.locator('[data-key="amount"]');
      await expect(amountField.locator("#f_contract_words_amount")).toHaveValue(
        override,
      );
      await amountField.getByRole("button", { name: "恢复自动" }).click();
      await expect(amountField.locator(".money-words-auto")).toContainText(
        "人民币壹万贰仟伍佰元叁角肆分",
      );
      await dialog.getByRole("button", { name: "保存", exact: true }).click();
      await expect(dialog).toBeHidden();

      const detail = await getJSON<{ contract: ContractItem }>(
        page,
        app,
        `/contracts/${contractID}`,
      );
      expect(detail.contract.data.__amount_words__ ?? "").toBe("");
      await shot(page, testInfo, "03-清空覆盖恢复自动");
    });

    await test.step("续签带出覆盖值并随新合同保存", async () => {
      // 重新加载后再打开：台账列表刷新是异步的，直接复用旧行会带着清空前的
      // 覆盖值进入表单（E2E 竞态，与业务无关）。
      await page.reload({ waitUntil: "domcontentloaded" });
      await page.locator('#tabs button[data-tab="contracts"]').click();
      await expect(page.locator("#view-contracts")).toHaveClass(/active/);
      const dialog = await editContract(page, contractTitle);
      const amountField = dialog.locator('[data-key="amount"]');
      await amountField.getByRole("button", { name: "改", exact: true }).click();
      await amountField.locator("#f_contract_words_amount").fill(override);
      await dialog.getByRole("button", { name: "保存", exact: true }).click();
      await expect(dialog).toBeHidden();

      const editDialog = await editContract(page, contractTitle);
      await editDialog.getByRole("button", { name: "续签", exact: true }).click();
      const renewDialog = page
        .getByRole("dialog")
        .filter({ has: page.getByRole("heading", { name: /新建合同 · 续签自/ }) });
      await expect(renewDialog).toBeVisible();
      const renewAmount = renewDialog.locator('[data-key="amount"]');
      await expect(
        renewAmount.locator("#f_contract_words_amount"),
      ).toHaveValue(override);
      await renewDialog.getByRole("button", { name: "保存", exact: true }).click();

      // 续签保存后界面直接打开新合同编辑框（onCreated）。
      const createdDialog = page
        .getByRole("dialog")
        .filter({ has: page.getByRole("heading", { name: /编辑合同/ }) });
      await expect(createdDialog).toBeVisible();

      const list = await getJSON<{ items: ContractItem[] }>(
        page,
        app,
        `/contracts?limit=100`,
      );
      const renewed = list.items.find(
        (item) => item.source_contract_id === contractID,
      );
      expect(renewed).toBeTruthy();
      expect(renewed?.data.__amount_words__).toBe(override);
      await shot(page, testInfo, "04-续签保留手填覆盖");
    });

    expect(
      consoleErrors,
      `浏览器控制台出现错误：\n${consoleErrors.join("\n")}`,
    ).toEqual([]);
  });
});

test.describe("场景 G2：模板绑定金额大写后生成 PDF", () => {
  test("自动大写与手填覆盖写入 PDF，清空覆盖恢复自动", async ({
    app,
    page,
  }, testInfo) => {
    const consoleErrors = trackConsoleErrors(page);
    const stamp = Date.now().toString(36);
    const templateName = `E2E 大写模板 ${stamp}`;
    const contractTitle = `E2E 大写 PDF 合同 ${stamp}`;

    const partyID = await seedParty(page, app, stamp);
    const money = await seedMoneyField(page, app, stamp);

    const pdfBytes = await readFile(
      join(repoRoot(), "e2e", "fixtures", "template.pdf"),
    );
    const imported = await postJSON<{ item: { id: string } }>(
      page,
      app,
      "/templates/import",
      {
        name: templateName,
        file_name: "words-template.pdf",
        file_base64: pdfBytes.toString("base64"),
      },
    );
    const mapped = await putJSON<{ item: { id: string } }>(
      page,
      app,
      `/templates/${imported.item.id}/mapping`,
      {
        placements: [
          {
            id: "words-amount",
            page: 1,
            x: 50,
            y: 80,
            w: 420,
            h: 30,
            type: "field",
            field: "contract.amount.words",
            font_size: 0,
            align: "left",
          },
          {
            id: "words-money",
            page: 1,
            x: 50,
            y: 130,
            w: 420,
            h: 30,
            type: "field",
            field: `contract.data.${money.key}.words`,
            font_size: 0,
            align: "left",
          },
        ],
      },
    );
    const templateID = mapped.item.id;

    const created = await postJSON<{ item: ContractItem }>(
      page,
      app,
      "/contracts",
      {
        year: new Date().getFullYear(),
        title: contractTitle,
        party_b_id: partyID,
        status: "draft",
        amount: 1500.5,
        data: { [money.key]: 1000.05 },
      },
    );
    const contractID = created.item.id;

    await page.goto(`${app.baseURL}/app/`, { waitUntil: "domcontentloaded" });

    async function renderPDF(): Promise<string> {
      const preview = await postJSON<{ preview_sha256: string }>(
        page,
        app,
        `/templates/${templateID}/preview`,
        { contract_id: contractID },
      );
      const generated = await postJSON<{ item: { id: string } }>(
        page,
        app,
        `/templates/${templateID}/generate`,
        { contract_id: contractID, preview_sha256: preview.preview_sha256 },
      );
      const file = await page.request.get(
        `${app.baseURL}/app/api/v1/documents/${generated.item.id}/file`,
      );
      expect(file.ok()).toBeTruthy();
      const analysis = await analyzePdf(page, await file.body());
      return analysis.text.replace(/\s+/g, "");
    }

    async function updateData(data: Record<string, unknown>): Promise<void> {
      await putJSON(page, app, `/contracts/${contractID}`, {
        year: new Date().getFullYear(),
        title: contractTitle,
        party_b_id: partyID,
        status: "draft",
        amount: 1500.5,
        data,
      });
    }

    await test.step("未覆盖时 PDF 使用自动大写", async () => {
      const text = await renderPDF();
      expect(text).toContain("人民币壹仟伍佰元伍角");
      expect(text).toContain("人民币壹仟元零伍分");
      await shot(page, testInfo, "01-自动大写已写入PDF");
    });

    await test.step("手填覆盖后 PDF 使用覆盖值", async () => {
      await updateData({
        [money.key]: 1000.05,
        __amount_words__: "人民币捌佰捌拾捌元整",
        [`__words__${money.key}`]: "人民币柒佰柒拾柒元整",
      });
      const text = await renderPDF();
      expect(text).toContain("人民币捌佰捌拾捌元整");
      expect(text).toContain("人民币柒佰柒拾柒元整");
      expect(text).not.toContain("人民币壹仟伍佰元伍角");
      await shot(page, testInfo, "02-覆盖值已写入PDF");
    });

    await test.step("清空覆盖后 PDF 恢复自动大写", async () => {
      await updateData({
        [money.key]: 1000.05,
        __amount_words__: "",
        [`__words__${money.key}`]: "",
      });
      const detail = await getJSON<{ contract: ContractItem }>(
        page,
        app,
        `/contracts/${contractID}`,
      );
      expect(detail.contract.data.__amount_words__ ?? "").toBe("");
      const text = await renderPDF();
      expect(text).toContain("人民币壹仟伍佰元伍角");
      expect(text).toContain("人民币壹仟元零伍分");
      await shot(page, testInfo, "03-清空覆盖恢复自动");
    });

    expect(
      consoleErrors,
      `浏览器控制台出现错误：\n${consoleErrors.join("\n")}`,
    ).toEqual([]);
  });
});
