// 真实业务场景的固定业务数据与标定清单（全部虚构，仅用于演示与测试）。
//
// 场景：幸福村股份经济合作社（出租方）把「幸福村 3 组 12 号 101」租给
// 王建国（承租方），租期 2026-12-01 ~ 2027-11-30。模板取自 v0.3 租赁定稿
// （`plans/v03-lease/房屋租赁合同-定稿.docx`，3 页、24 个标定位置），
// 锚点文字以 `plans/v03-lease/模板定稿-绑定清单.md` 为准。
import type { PlacementKind } from "./business-calibrate";

export const TEMPLATE_NAME = "房屋租赁合同（幸福村模板）";
export const CONTRACT_TITLE = "房屋租赁合同（幸福村—王建国）";
export const PARTY_A_NAME = "幸福村股份经济合作社";
export const PARTY_A_ID_NUMBER = "91330100MA2XXXXX0A";
export const PARTY_A_PHONE = "0571-88880001";
export const PARTY_A_ADDRESS = "幸福村村委会办公楼";
export const PARTY_B_NAME = "王建国";
export const PARTY_B_ID_NUMBER = "330102198001010011";
export const PARTY_B_PHONE = "13900000001";
export const PARTY_B_ADDRESS = "幸福村 3 组 12 号";
export const ASSET_NAME = "幸福村 3 组 12 号";
export const ASSET_ADDRESS = "幸福村 3 组 12 号";
export const ASSET_ROOM_NO = "101";
export const ASSET_AREA = "80";
export const MONTHLY_RENT = "1500";
export const MONTHLY_RENT_DISPLAY = "1,500.00";
/** A 珠自动生成的中文大写（1500 → 人民币壹仟伍佰元整）。 */
export const MONTHLY_RENT_WORDS = "人民币壹仟伍佰元整";
export const DEPOSIT = "1500";
export const DEPOSIT_DISPLAY = "1,500.00";
export const DEPOSIT_WORDS = "人民币壹仟伍佰元整";
export const PENALTY = "1500";
export const LEASE_START = "2026-12-01";
export const LEASE_END = "2027-11-30";
export const SIGNED_ON = "2026-11-26";
export const DELIVERY_DATE = "2026-12-01";
export const SEAL_NAME = "幸福村股份经济合作社合同专用章";

export interface FieldDraft {
  entity: "合同字段" | "相对方字段";
  label: string;
  key: string;
  type: "text" | "number" | "money" | "date";
  /** 合同表单分组（B 珠能力）：租期 / 租金与押金 / 费用与违约 / 其他约定。 */
  group?: string;
}

/**
 * 定稿清单的 12 个动态字段（键与模板定稿-绑定清单.md 一致，全部建在
 * 「合同字段」）。其中月租金大写/押金大写是清单口径的纯文本字段；模板里
 * 两个大写位置实际绑定 A 珠自动大写（`contract.data.<金额键>.words`），
 * 填写合同时不需要手填这两个文本字段。
 */
export const CONTRACT_FIELDS: FieldDraft[] = [
  { entity: "合同字段", label: "租赁用途", key: "rental_use", type: "text", group: "其他约定" },
  { entity: "合同字段", label: "月租金", key: "monthly_rent", type: "money", group: "租金与押金" },
  { entity: "合同字段", label: "月租金大写", key: "monthly_rent_words", type: "text", group: "租金与押金" },
  { entity: "合同字段", label: "付款周期", key: "payment_cycle", type: "text", group: "租金与押金" },
  { entity: "合同字段", label: "押金", key: "deposit", type: "money", group: "租金与押金" },
  { entity: "合同字段", label: "押金大写", key: "deposit_words", type: "text", group: "租金与押金" },
  { entity: "合同字段", label: "交付日期", key: "delivery_date", type: "date", group: "租期" },
  { entity: "合同字段", label: "提前退租通知天数", key: "early_notice_days", type: "number", group: "费用与违约" },
  { entity: "合同字段", label: "逾期天数", key: "overdue_days", type: "number", group: "费用与违约" },
  { entity: "合同字段", label: "违约金", key: "penalty", type: "money", group: "费用与违约" },
  { entity: "合同字段", label: "费用承担方", key: "fee_bearer", type: "text", group: "费用与违约" },
  { entity: "合同字段", label: "其他约定", key: "other_terms", type: "text", group: "其他约定" },
];

export interface PlacementDraft {
  label: string;
  kind: PlacementKind;
  binding?: string;
  party?: "party_a" | "party_b";
}

/**
 * 定稿模板的 24 个标定位置；标签必须全文档唯一（`locateAnchors` 会据此
 * 在转换后的 PDF 文本层里定位）。两个大写位置绑定 A 珠自动大写绑定键。
 */
export const PLACEMENTS: PlacementDraft[] = [
  { label: "出租方（甲方）：", kind: "field", binding: "party_a.name" },
  { label: "证件号/统一社会信用代码：", kind: "field", binding: "party_a.id_number" },
  { label: "承租方（乙方）：", kind: "field", binding: "party_b.name" },
  { label: "身份证号：", kind: "field", binding: "party_b.id_number" },
  { label: "坐落于", kind: "field", binding: "contract.asset_address" },
  { label: "房号", kind: "field", binding: "contract.asset_room_no" },
  { label: "建筑面积", kind: "field", binding: "contract.asset_area" },
  { label: "租赁期自", kind: "field", binding: "contract.lease_start" },
  { label: "起至", kind: "field", binding: "contract.lease_end" },
  { label: "止。甲方应于", kind: "field", binding: "contract.data.delivery_date" },
  { label: "签订日期：", kind: "field", binding: "contract.signed_on" },
  { label: "月租金为人民币", kind: "field", binding: "contract.data.monthly_rent" },
  { label: "（月租金大写：", kind: "field", binding: "contract.data.monthly_rent.words" },
  { label: "租金按", kind: "field", binding: "contract.data.payment_cycle" },
  { label: "押金人民币", kind: "field", binding: "contract.data.deposit" },
  { label: "（押金大写：", kind: "field", binding: "contract.data.deposit.words" },
  { label: "等费用由", kind: "field", binding: "contract.data.fee_bearer" },
  { label: "支付违约金人民币", kind: "field", binding: "contract.data.penalty" },
  { label: "租赁用途：", kind: "field", binding: "contract.data.rental_use" },
  { label: "应提前", kind: "field", binding: "contract.data.early_notice_days" },
  { label: "拖欠租金超过", kind: "field", binding: "contract.data.overdue_days" },
  { label: "其他约定：", kind: "field", binding: "contract.data.other_terms" },
  { label: "甲方（签字/盖章）：", kind: "seal", party: "party_a" },
  { label: "乙方（签字）：", kind: "signature", party: "party_b" },
];

export const PARTY_A_VALUES: Record<string, string> = {
  name: PARTY_A_NAME,
  kind: "单位",
  id_number: PARTY_A_ID_NUMBER,
  phone: PARTY_A_PHONE,
  address: PARTY_A_ADDRESS,
};

export const PARTY_B_VALUES: Record<string, string> = {
  name: PARTY_B_NAME,
  kind: "个人",
  id_number: PARTY_B_ID_NUMBER,
  phone: PARTY_B_PHONE,
  address: PARTY_B_ADDRESS,
};

export const ASSET_VALUES = {
  name: ASSET_NAME,
  address: ASSET_ADDRESS,
  room_no: ASSET_ROOM_NO,
  area: ASSET_AREA,
};

/** 合同动态字段取值；两个大写字段留给 A 珠自动大写，不手填。 */
export const CONTRACT_DATA: Record<string, string> = {
  rental_use: "居住",
  monthly_rent: MONTHLY_RENT,
  payment_cycle: "按年",
  deposit: DEPOSIT,
  delivery_date: DELIVERY_DATE,
  early_notice_days: "30",
  overdue_days: "15",
  penalty: PENALTY,
  fee_bearer: "乙方",
  other_terms: "无",
};

export const CONTRACT_VALUES = {
  title: CONTRACT_TITLE,
  type: "房屋租赁",
  partyB: PARTY_B_NAME,
  amount: MONTHLY_RENT,
  signedOn: SIGNED_ON,
  leaseStart: LEASE_START,
  leaseEnd: LEASE_END,
  validFrom: LEASE_START,
  validTo: LEASE_END,
};

/**
 * 最终 PDF 必须包含的关键业务值（按模板实际渲染结果）。
 *
 * 定稿模板没有「合同编号」位置，合同编号由台账/生成记录核对，不在这里断言；
 * 目录里的 24 个位置只覆盖双方档案、房产、租期与租金押金等条款。
 */
export function requiredPdfValues(): string[] {
  const compact = (value: string): string => value.replace(/\s+/g, "");
  return [
    PARTY_A_NAME,
    PARTY_A_ID_NUMBER,
    PARTY_B_NAME,
    PARTY_B_ID_NUMBER,
    // 套印值追加在模板文字之后，按标定顺序相邻；地址+房号+面积合成断言，
    // 避免「101/80」误中证件号里的数字。
    `${compact(ASSET_ADDRESS)}${ASSET_ROOM_NO}${ASSET_AREA}`,
    "2026年12月1日",
    "2027年11月30日",
    "2026年11月26日",
    MONTHLY_RENT_DISPLAY,
    MONTHLY_RENT_WORDS,
    "按年",
    "居住",
  ];
}
