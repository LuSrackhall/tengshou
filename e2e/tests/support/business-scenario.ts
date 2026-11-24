// 真实业务场景的固定业务数据与标定清单（全部虚构，仅用于演示与测试）。
import type { PlacementKind } from "./business-calibrate";

export const TEMPLATE_NAME = "农产品购销合同（幸福村模板）";
export const CONTRACT_TITLE = "农产品购销合同（幸福村—王建国）";
export const PARTY_A_NAME = "幸福村股份经济合作社";
export const PARTY_B_NAME = "王建国";
export const AMOUNT = "160000";
export const AMOUNT_DISPLAY = "160,000.00";
export const AMOUNT_WORDS = "人民币壹拾陆万元整";
export const SEAL_NAME = "幸福村股份经济合作社合同专用章";

export interface FieldDraft {
  entity: "合同字段" | "相对方字段";
  label: string;
  key: string;
  type: "text" | "number" | "money";
}

export const CONTRACT_FIELDS: FieldDraft[] = [
  { entity: "合同字段", label: "标的名称", key: "product_name", type: "text" },
  { entity: "合同字段", label: "规格", key: "spec", type: "text" },
  { entity: "合同字段", label: "数量（公斤）", key: "quantity", type: "number" },
  { entity: "合同字段", label: "单价（元/公斤）", key: "unit_price", type: "money" },
  { entity: "合同字段", label: "大写金额", key: "amount_words", type: "text" },
  { entity: "合同字段", label: "交货时间", key: "delivery_time", type: "text" },
  { entity: "合同字段", label: "交货地点", key: "delivery_place", type: "text" },
  { entity: "合同字段", label: "验收标准", key: "acceptance_standard", type: "text" },
  { entity: "合同字段", label: "付款方式", key: "payment_terms", type: "text" },
  { entity: "合同字段", label: "违约责任", key: "penalty_terms", type: "text" },
  { entity: "合同字段", label: "争议解决", key: "dispute_resolution", type: "text" },
  { entity: "相对方字段", label: "联系人", key: "contact", type: "text" },
];

export interface PlacementSpec {
  label: string;
  kind: PlacementKind;
  binding?: string;
  party?: "party_a" | "party_b";
}

/** 与 docx 中「字段名：＿＿」逐行对应；标签必须全文档唯一。 */
export const PLACEMENTS: PlacementSpec[] = [
  { label: "合同编号：", kind: "field", binding: "contract.contract_no" },
  { label: "合同名称：", kind: "field", binding: "contract.title" },
  { label: "签订日期：", kind: "field", binding: "contract.signed_on" },
  { label: "甲方名称：", kind: "field", binding: "party_a.name" },
  { label: "甲方证件号：", kind: "field", binding: "party_a.id_number" },
  { label: "甲方地址：", kind: "field", binding: "party_a.address" },
  { label: "甲方电话：", kind: "field", binding: "party_a.phone" },
  { label: "甲方联系人：", kind: "field", binding: "party_a.data.contact" },
  { label: "乙方姓名：", kind: "field", binding: "party_b.name" },
  { label: "乙方证件号：", kind: "field", binding: "party_b.id_number" },
  { label: "乙方地址：", kind: "field", binding: "party_b.address" },
  { label: "乙方电话：", kind: "field", binding: "party_b.phone" },
  { label: "标的名称：", kind: "field", binding: "contract.data.product_name" },
  { label: "规格：", kind: "field", binding: "contract.data.spec" },
  { label: "数量：", kind: "field", binding: "contract.data.quantity" },
  { label: "单价：", kind: "field", binding: "contract.data.unit_price" },
  { label: "小写金额：", kind: "field", binding: "contract.amount" },
  { label: "大写金额：", kind: "field", binding: "contract.data.amount_words" },
  { label: "交货时间：", kind: "field", binding: "contract.data.delivery_time" },
  { label: "交货地点：", kind: "field", binding: "contract.data.delivery_place" },
  { label: "验收标准：", kind: "field", binding: "contract.data.acceptance_standard" },
  { label: "付款方式：", kind: "field", binding: "contract.data.payment_terms" },
  { label: "违约责任：", kind: "field", binding: "contract.data.penalty_terms" },
  { label: "争议解决：", kind: "field", binding: "contract.data.dispute_resolution" },
  { label: "甲方（盖章）：", kind: "seal", party: "party_a" },
  { label: "乙方（签字）：", kind: "signature", party: "party_b" },
];

export const PARTY_A_VALUES: Record<string, string> = {
  name: PARTY_A_NAME,
  kind: "单位",
  id_number: "91330100MA2XXXXX0A",
  phone: "0571-88880001",
  address: "幸福村村委会办公楼",
  contact: "李国强",
};

export const PARTY_B_VALUES: Record<string, string> = {
  name: PARTY_B_NAME,
  kind: "个人",
  id_number: "330102198001010011",
  phone: "13900000001",
  address: "幸福村3组12号",
};

export const CONTRACT_DATA: Record<string, string> = {
  product_name: "红富士苹果",
  spec: "一级果",
  quantity: "20000",
  unit_price: "8.00",
  amount_words: AMOUNT_WORDS,
  delivery_time: "2026年12月10日前",
  delivery_place: "幸福村合作社仓库",
  acceptance_standard: "按一级果标准现场验收",
  payment_terms: "验收合格后7日内转账",
  penalty_terms: "按未履行部分货款的5%支付违约金",
  dispute_resolution: "协商不成由当地法院管辖",
};

export const CONTRACT_VALUES = {
  title: CONTRACT_TITLE,
  type: "农产品购销",
  partyB: PARTY_B_NAME,
  amount: AMOUNT,
  signedOn: "2026-11-24",
  validFrom: "2026-11-24",
  validTo: "2026-12-31",
};

/** 最终 PDF 必须包含的关键业务值。 */
export function requiredPdfValues(contractNo: string): string[] {
  return [
    contractNo,
    AMOUNT_DISPLAY,
    PARTY_A_NAME,
    PARTY_B_NAME,
    "红富士苹果",
    AMOUNT_WORDS,
    "幸福村合作社仓库",
    "一级果",
  ];
}
