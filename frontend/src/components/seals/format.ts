// 公章域的展示与文件工具：逐条对应存量 app.js 的同名逻辑。
import type { Party, PartyKind, Seal } from "../../api/types";

/** 卡片/预览里的归属文案；单位已删除时与迁移前一样回落「单位档案」。 */
export function sealOwner(seal: Seal, parties: Party[]): string {
  if (!seal.party_id) {
    return "未关联单位";
  }
  return parties.find((party) => party.id === seal.party_id)?.name || "单位档案";
}

export function kindLabel(kind: PartyKind): string {
  return kind === "organization" ? "单位" : "个人";
}

/** 模板占位片段；输出与 app.js sealPlacementSnippet 逐字一致。 */
export function sealPlacementSnippet(
  seal: Seal,
  widthMM: number,
  align: string,
): string {
  const source = seal.party_id
    ? "sealPath .Seals .PartyA"
    : "contractSealPath .ContractSeals";
  const textAlign = align === "left" ? "left" : align === "right" ? "right" : "center";
  return `{{with ${source}}}<p style="text-align:${textAlign}"><img src="{{.}}" style="width:${widthMM}mm"></p>{{end}}`;
}

/** FileReader → base64（去掉 data: 前缀）；错误文案与 app.js 一致。 */
export function readFileBase64(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => {
      const result = String(reader.result || "");
      const idx = result.indexOf(",");
      resolve(idx >= 0 ? result.slice(idx + 1) : result);
    };
    reader.onerror = () => reject(new Error("读取文件失败"));
    reader.readAsDataURL(file);
  });
}

/** 复制文本；非安全上下文回落 execCommand（与 app.js copyText 相同）。 */
export async function copyText(text: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    const area = document.createElement("textarea");
    area.value = text;
    document.body.appendChild(area);
    area.select();
    let ok = false;
    try {
      ok = document.execCommand("copy");
    } catch {
      /* ignore */
    }
    area.remove();
    return ok;
  }
}
