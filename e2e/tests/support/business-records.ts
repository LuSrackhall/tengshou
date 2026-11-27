// 台账域动作：合同字段与业务分组、相对方档案、房产档案、新建合同草稿。
// 选择器与操作细节集中在这里；分镜采集通过可选的 FrameRecorder 注入。
import { expect, type Page } from "@playwright/test";
import { appUrl } from "./business-api";
import type { FrameRecorder } from "./business-frames";
import {
  ASSET_NAME,
  ASSET_VALUES,
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
  let groupCaptured = false;
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
    // 字段键默认按名称自动生成；业务模板绑定固定英文键，展开高级选项手填。
    await dialog.locator("details.advanced-options summary").click();
    await dialog.locator("#fieldKey").fill(field.key);
    await dialog.locator("#fieldType").selectOption(field.type);
    if (field.group) {
      // B 珠能力：合同表单按分组展示；可选预设，也可自定义。
      await dialog.locator("#fieldGroup").fill(field.group);
    }
    if (!firstSaved) {
      await frames?.capture(
        `② 示例：标签=${field.label}，键=${field.key}，类型=${field.type}，分组=${field.group ?? "（空）"}`,
        {
          region: dialog,
          targets: [
            dialog.locator("#fieldLabel"),
            dialog.locator("#fieldKey"),
            dialog.locator("#fieldType"),
            dialog.locator("#fieldGroup"),
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
    if (field.group === "租金与押金" && !groupCaptured) {
      await frames?.capture(
        `④ 分组写入字段：${field.label} 归入「${field.group}」`,
        { targets: [row] },
      );
      groupCaptured = true;
    }
  }
  const contractCount = drafts.filter((item) => item.entity === "合同字段").length;
  await page
    .locator("#view-fields .segmented")
    .getByRole("button", { name: "合同字段", exact: true })
    .click();
  await expect(
    page.locator('#view-fields tr[data-id]:has-text("租赁用途")'),
  ).toBeVisible();
  await frames?.capture(
    `⑤ 共 ${contractCount} 个合同字段，按租期 / 租金与押金 / 费用与违约 / 其他约定分组`,
    { region: page.locator("#view-fields") },
  );
  return (
    `已建立 ${contractCount} 个租赁合同字段（租赁用途、月租金/押金及大写、付款周期、` +
    "交付日期、提前退租通知天数、逾期天数、违约金、费用承担方、其他约定），" +
    "并按租期 / 租金与押金 / 费用与违约 / 其他约定分组。"
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
    await frames?.capture(`② 录入出租方（单位）：${values.name}`, {
      region: dialog,
      targets: [
        dialog.locator("#f_party_name"),
        dialog.locator("#f_party_id_number"),
        dialog.locator("#f_party_address"),
        dialog.locator("#f_party_phone"),
      ],
    });
  } else {
    await frames?.capture(`④ 同一方式建立承租方（个人）${values.name}`, {
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
    await frames?.capture("③ 出租方档案已保存", { targets: [row] });
  }
  return id;
}

/** 新建出租房产档案（地址/房号/面积），返回房产 id。 */
export async function createAsset(
  page: Page,
  frames?: FrameRecorder,
): Promise<string> {
  await page.locator('#tabs button[data-tab="assets"]').click();
  await expect(page.locator("#view-assets")).toHaveClass(/active/);
  await frames?.capture("① 打开『房产档案』，点击『新建房产』", {
    targets: [page.locator("#newAssetBtn")],
  });
  await page.locator("#newAssetBtn").click();
  const dialog = page
    .getByRole("dialog")
    .filter({ has: page.getByRole("heading", { name: "新建房产", exact: true }) });
  await expect(dialog).toBeVisible();
  await dialog.locator("#f_asset_name").fill(ASSET_VALUES.name);
  await dialog.locator("#f_asset_address").fill(ASSET_VALUES.address);
  await dialog.locator("#f_asset_room_no").fill(ASSET_VALUES.room_no);
  await dialog.locator("#f_asset_area").fill(ASSET_VALUES.area);
  await frames?.capture(
    `② 录入出租房产：${ASSET_VALUES.name} 房号 ${ASSET_VALUES.room_no}，${ASSET_VALUES.area} 平方米`,
    {
      region: dialog,
      targets: [
        dialog.locator("#f_asset_name"),
        dialog.locator("#f_asset_address"),
        dialog.locator("#f_asset_room_no"),
        dialog.locator("#f_asset_area"),
      ],
    },
  );
  await dialog.getByRole("button", { name: "保存", exact: true }).click();
  await expect(dialog).toBeHidden();
  const row = page.locator(`#assetTable tr[data-id]:has-text("${ASSET_NAME}")`);
  await expect(row).toBeVisible();
  const id = (await row.getAttribute("data-id")) ?? "";
  expect(id).not.toBe("");
  await frames?.capture("③ 房产档案就绪，新建合同时可直接选择", {
    targets: [row],
  });
  return id;
}

export interface ContractDetailView {
  contract: { contract_no: string; status: string; amount: number };
  party_a?: { name: string };
  party_b?: { name: string };
  asset?: { name: string; room_no: string };
}

export interface CreatedContract {
  contractId: string;
  contractNo: string;
  detail: ContractDetailView;
}

/**
 * 关闭标定编辑器，在合同页新建草稿：甲方下拉选择出租方、乙方从档案选择、
 * 房产从房产档案选择，再填租期与租金押金，核对服务端返回的三方与金额。
 */
export async function createContract(
  page: Page,
  partyAId: string,
  assetId: string,
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
  const partyB = dialog.locator('.picker-field[data-key="party_b"]');
  await partyB.locator("#f_contract_party_b").fill(CONTRACT_VALUES.partyB);
  await partyB
    .locator(`#partyBPickerList button[data-party-id]:has-text("${CONTRACT_VALUES.partyB}")`)
    .click();
  await expect(partyB.locator(".picker-selected")).toContainText(
    CONTRACT_VALUES.partyB,
  );
  const asset = dialog.locator('.picker-field[data-key="asset"]');
  await asset.locator("#f_contract_asset").fill(ASSET_NAME);
  await asset
    .locator(`#assetPickerList button[data-asset-id]:has-text("${ASSET_NAME}")`)
    .click();
  await expect(asset.locator(".picker-selected")).toContainText(ASSET_NAME);
  await frames?.capture(
    `② 三方选择：出租方=${PARTY_A_NAME}，承租方=${CONTRACT_VALUES.partyB}，房产=${ASSET_NAME}`,
    {
      region: dialog,
      targets: [
        dialog.locator("#f_contract_party_a"),
        partyB,
        asset,
      ],
    },
  );
  await dialog.locator("#f_contract_amount").fill(CONTRACT_VALUES.amount);
  await dialog.locator("#f_contract_signed_on").fill(CONTRACT_VALUES.signedOn);
  await dialog.locator("#f_contract_lease_start").fill(CONTRACT_VALUES.leaseStart);
  await dialog.locator("#f_contract_lease_end").fill(CONTRACT_VALUES.leaseEnd);
  await dialog.locator("#f_contract_valid_from").fill(CONTRACT_VALUES.validFrom);
  await dialog.locator("#f_contract_valid_to").fill(CONTRACT_VALUES.validTo);
  for (const [key, value] of Object.entries(CONTRACT_DATA)) {
    await dialog.locator(`#f_contract_${key}`).fill(value);
  }
  const rentField = dialog.locator('.field[data-key="monthly_rent"]');
  await frames?.capture(
    `③ 租期 ${CONTRACT_VALUES.leaseStart} → ${CONTRACT_VALUES.leaseEnd}；` +
      "月租金/押金填好后，下方自动生成中文大写（可点「改」手填覆盖）",
    {
      region: dialog,
      targets: [
        dialog.locator("#f_contract_lease_start"),
        dialog.locator("#f_contract_lease_end"),
        rentField,
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
  expect(detail.asset?.name).toBe(ASSET_NAME);
  return { contractId, contractNo: detail.contract.contract_no, detail };
}
