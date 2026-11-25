// 台账域动作：合同字段与相对方档案、新建合同草稿。
// 选择器与操作细节集中在这里；分镜采集通过可选的 FrameRecorder 注入。
import { expect, type Page } from "@playwright/test";
import { appUrl } from "./business-api";
import type { FrameRecorder } from "./business-frames";
import {
  CONTRACT_DATA,
  CONTRACT_FIELDS,
  CONTRACT_VALUES,
  PARTY_A_NAME,
  PARTY_B_NAME,
  type FieldDraft,
} from "./business-scenario";

export async function createFields(
  page: Page,
  frames?: FrameRecorder,
  drafts: FieldDraft[] = CONTRACT_FIELDS,
): Promise<string> {
  await page.locator('#tabs button[data-tab="fields"]').click();
  await expect(page.locator("#view-fields")).toHaveClass(/active/);
  const newButton = page
    .locator("#view-fields")
    .getByRole("button", { name: "新建字段", exact: true });
  await frames?.capture("① 打开『字段』页，点击『新建字段』", {
    targets: [newButton],
  });
  let firstSaved = false;
  for (const field of drafts) {
    await page
      .locator("#view-fields .segmented")
      .getByRole("button", { name: field.entity, exact: true })
      .click();
    await newButton.click();
    const dialog = page
      .getByRole("dialog")
      .filter({
        has: page.getByRole("heading", { name: "新建字段", exact: true }),
      });
    await expect(dialog).toBeVisible();
    await dialog.locator("#fieldLabel").fill(field.label);
    await dialog.locator("#fieldKey").fill(field.key);
    await dialog.locator("#fieldType").selectOption(field.type);
    if (!firstSaved) {
      await frames?.capture(
        `② 示例：标签=${field.label}，键=${field.key}，类型=${field.type}`,
        {
          region: dialog,
          targets: [
            dialog.locator("#fieldLabel"),
            dialog.locator("#fieldKey"),
            dialog.locator("#fieldType"),
          ],
        },
      );
    }
    await dialog.getByRole("button", { name: "保存", exact: true }).click();
    await expect(dialog).toBeHidden();
    const row = page.locator(
      `#view-fields tr[data-id]:has-text("${field.label}")`,
    );
    await expect(row).toBeVisible();
    if (!firstSaved) {
      await frames?.capture("③ 保存后列表立即出现该字段", { targets: [row] });
      firstSaved = true;
    }
    if (field.entity === "相对方字段") {
      await frames?.capture(
        "④ 同样方式建立相对方字段（联系人），保存后立即可用",
        { targets: [row] },
      );
    }
  }
  const contractCount = drafts.filter((item) => item.entity === "合同字段").length;
  await page
    .locator("#view-fields .segmented")
    .getByRole("button", { name: "合同字段", exact: true })
    .click();
  await expect(
    page.locator('#view-fields tr[data-id]:has-text("标的名称")'),
  ).toBeVisible();
  await frames?.capture(
    `⑤ 共 ${contractCount} 个合同字段 + 1 个相对方字段，全部保存成功（当前列表为合同字段）`,
    { region: page.locator("#view-fields") },
  );
  return (
    `已建立合同字段 ${contractCount} 个（标的名称、规格、数量、单价、大写金额、` +
    "交货时间/地点、验收标准、付款方式、违约责任、争议解决）与相对方「联系人」字段。"
  );
}

/** 新建一份相对方档案，返回档案 id。 */
export async function createParty(
  page: Page,
  values: Record<string, string>,
  frames?: FrameRecorder,
): Promise<string> {
  await page.locator('#tabs button[data-tab="parties"]').click();
  await expect(page.locator("#view-parties")).toHaveClass(/active/);
  if (values.name === PARTY_A_NAME) {
    await frames?.capture("① 打开『相对方档案』，点击『新建相对方』", {
      targets: [page.locator("#newPartyBtn")],
    });
  }
  await page.locator("#newPartyBtn").click();
  const dialog = page
    .getByRole("dialog")
    .filter({
      has: page.getByRole("heading", { name: "新建相对方", exact: true }),
    });
  await expect(dialog).toBeVisible();
  for (const [key, value] of Object.entries(values)) {
    if (key === "kind") {
      await dialog.locator("#f_party_kind").selectOption(value);
    } else {
      await dialog.locator(`#f_party_${key}`).fill(value);
    }
  }
  if (values.name === PARTY_A_NAME) {
    await frames?.capture(`② 录入甲方（单位）：${values.name}`, {
      region: dialog,
      targets: [
        dialog.locator("#f_party_name"),
        dialog.locator("#f_party_id_number"),
        dialog.locator("#f_party_address"),
        dialog.locator("#f_party_phone"),
      ],
    });
  } else {
    await frames?.capture(`④ 同一方式建立乙方（个人）${values.name}`, {
      region: dialog,
      targets: [
        dialog.locator("#f_party_name"),
        dialog.locator("#f_party_kind"),
        dialog.locator("#f_party_id_number"),
      ],
    });
  }
  await dialog.getByRole("button", { name: "保存", exact: true }).click();
  await expect(dialog).toBeHidden();
  const row = page.locator(
    `#partyTable tr[data-id]:has-text("${values.name}")`,
  );
  await expect(row).toBeVisible();
  const id = (await row.getAttribute("data-id")) ?? "";
  expect(id).not.toBe("");
  if (values.name === PARTY_A_NAME) {
    await frames?.capture("③ 甲方档案已保存", { targets: [row] });
  }
  return id;
}


export interface ContractDetailView {
  contract: { contract_no: string; status: string; amount: number };
  party_a?: { name: string };
  party_b?: { name: string };
}

export interface CreatedContract {
  contractId: string;
  contractNo: string;
  detail: ContractDetailView;
}

/** 关闭标定编辑器，在合同页新建草稿并核对服务端返回的甲方/相对方。 */
export async function createContract(
  page: Page,
  partyAId: string,
  frames?: FrameRecorder,
): Promise<CreatedContract> {
  await page.locator("#tplCloseBtn").click();
  await page.locator('#tabs button[data-tab="contracts"]').click();
  await expect(page.locator("#view-contracts")).toHaveClass(/active/);
  await frames?.capture("① 在『合同台账』点击『新建合同』", {
    targets: [page.locator("#newContractBtn")],
  });
  await page.locator("#newContractBtn").click();
  const dialog = page
    .getByRole("dialog")
    .filter({
      has: page.getByRole("heading", { name: "新建合同", exact: true }),
    });
  await expect(dialog).toBeVisible();
  await dialog.locator("#f_contract_party_a").selectOption(partyAId);
  await dialog.locator("#f_contract_title").fill(CONTRACT_VALUES.title);
  await dialog.locator("#f_contract_type").fill(CONTRACT_VALUES.type);
  await dialog.locator("#f_contract_party_b").fill(CONTRACT_VALUES.partyB);
  await frames?.capture(
    `② 选择甲方（本方）与相对方 ${CONTRACT_VALUES.partyB}`,
    {
      region: dialog,
      targets: [
        dialog.locator("#f_contract_party_a"),
        dialog.locator("#f_contract_party_b"),
      ],
    },
  );
  await dialog.locator("#f_contract_amount").fill(CONTRACT_VALUES.amount);
  await dialog.locator("#f_contract_signed_on").fill(CONTRACT_VALUES.signedOn);
  await dialog.locator("#f_contract_valid_from").fill(CONTRACT_VALUES.validFrom);
  await dialog.locator("#f_contract_valid_to").fill(CONTRACT_VALUES.validTo);
  for (const [key, value] of Object.entries(CONTRACT_DATA)) {
    await dialog.locator(`#f_contract_${key}`).fill(value);
  }
  await frames?.capture(
    `③ 填写标的：${CONTRACT_DATA.product_name} ${CONTRACT_DATA.quantity} 千克 × ${CONTRACT_DATA.unit_price} 元 = 160,000.00 元`,
    {
      region: dialog,
      targets: [
        dialog.locator("#f_contract_product_name"),
        dialog.locator("#f_contract_quantity"),
        dialog.locator("#f_contract_unit_price"),
        dialog.locator("#f_contract_amount"),
      ],
    },
  );
  await dialog.getByRole("button", { name: "保存", exact: true }).click();
  await expect(dialog).toBeHidden();
  const row = page.locator(
    `#contractTable tr[data-id]:has-text("${CONTRACT_VALUES.title}")`,
  );
  await expect(row).toBeVisible();
  await frames?.capture("④ 保存为草稿，编号自动分配", { targets: [row] });
  const contractId = (await row.getAttribute("data-id")) ?? "";
  expect(contractId).not.toBe("");
  const detail = (await (
    await page.request.get(appUrl(page, `/app/api/v1/contracts/${contractId}`))
  ).json()) as ContractDetailView;
  expect(detail.contract.status).toBe("draft");
  expect(detail.contract.amount).toBe(Number(CONTRACT_VALUES.amount));
  expect(detail.party_a?.name).toBe(PARTY_A_NAME);
  expect(detail.party_b?.name).toBe(PARTY_B_NAME);
  return { contractId, contractNo: detail.contract.contract_no, detail };
}

