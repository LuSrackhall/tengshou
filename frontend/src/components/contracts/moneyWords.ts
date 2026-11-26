// 金额中文大写（te-7c5v1）：与 internal/service/money_words.go 的 MoneyWords
// 保持同一算法与输出，供合同表单实时预览与手填覆盖回显使用。
//
// 保留键约定与服务端一致：内置金额用 __amount_words__，动态金额字段用
// __words__<字段键>；它们只存在 contract.data，不新增字段定义。

/** 合同内置金额（contract.amount）的大写覆盖键。 */
export const AMOUNT_WORDS_KEY = "__amount_words__";

/** 金额字段（内置 amount 或动态字段键）对应的大写覆盖保留键。 */
export function moneyWordsKey(fieldKey: string): string {
  return fieldKey === "amount" ? AMOUNT_WORDS_KEY : `__words__${fieldKey}`;
}

const DIGITS = ["零", "壹", "贰", "叁", "肆", "伍", "陆", "柒", "捌", "玖"];
const UNITS = ["", "拾", "佰", "仟"];
const GROUP_UNITS = ["", "万", "亿", "万亿"];

/**
 * 金额转中文大写：0、负数、非法输入与超出精确表示范围的金额返回空串
 * （与服务端一致）。1500 → 人民币壹仟伍佰元整；1500.55 → 人民币壹仟伍佰元伍角伍分。
 */
export function moneyWords(value: unknown): string {
  let amount: number;
  if (typeof value === "number") {
    amount = value;
  } else if (typeof value === "string") {
    const text = value.trim();
    if (text === "") {
      return "";
    }
    amount = Number(text);
  } else {
    return "";
  }
  if (!Number.isFinite(amount) || amount < 0) {
    return "";
  }
  const rounded = Math.round(amount * 100) / 100;
  if (rounded === 0) {
    return "";
  }
  // 与 Go 侧一致：整数部分超过 16 位视为不可精确表示的金额。
  if (rounded >= 1e16) {
    return "";
  }
  const text = rounded.toFixed(2);
  const dot = text.indexOf(".");
  const intPart = text.slice(0, dot);
  const frac = text.slice(dot + 1);
  const jiao = Number(frac[0] ?? "0");
  const fen = Number(frac[1] ?? "0");

  let out = "人民币";
  if (intPart !== "0") {
    out += intWords(intPart) + "元";
  }
  if (jiao === 0 && fen === 0) {
    out += "整";
  } else {
    if (jiao > 0) {
      out += (DIGITS[jiao] ?? "") + "角";
    } else if (intPart !== "0") {
      // 元与分之间有零：100.05 → 壹佰元零伍分。
      out += "零";
    }
    if (fen > 0) {
      out += (DIGITS[fen] ?? "") + "分";
    }
  }
  return out;
}

/** 十进制整数字符串（1–16 位）转大写，按 万/亿 四位分组。 */
function intWords(digits: string): string {
  const groups = Math.ceil(digits.length / 4);
  const padded = digits.padStart(groups * 4, "0");
  let out = "";
  for (let i = 0; i < groups; i += 1) {
    const group = padded.slice(i * 4, i * 4 + 4);
    if (group === "0000") {
      continue;
    }
    // 高位组之后紧跟一个小于 1000 的非零组时补一个零：壹万零壹。
    if (out !== "" && group[0] === "0") {
      out += "零";
    }
    out += groupWords(group) + (GROUP_UNITS[groups - 1 - i] ?? "");
  }
  return out;
}

/** 四位组（至少一位非零）的大写。 */
function groupWords(group: string): string {
  let out = "";
  let zero = false;
  for (let i = 0; i < 4; i += 1) {
    const digit = Number(group[i] ?? "0");
    if (digit === 0) {
      if (out !== "") {
        zero = true;
      }
      continue;
    }
    if (zero) {
      out += "零";
      zero = false;
    }
    out += (DIGITS[digit] ?? "") + (UNITS[3 - i] ?? "");
  }
  return out;
}
