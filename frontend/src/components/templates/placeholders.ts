// 模板占位符扫描：导入/打开 PDF 模板后，用 PDF.js 提取逐页文字，找出
// 【…】与连续下划线这类「草案占位符」。只做提示（数量 + 前 5 个示例），
// 不阻断保存、预览或生成。
//
// 坐标与契约无关：这里只读文本，不改标定位置；PDF.js 仍是 index.html 的本地
// vendor，不引入 npm 包、不访问 CDN。
import type { PdfDocument } from "./pdfjs";

export type PlaceholderKind = "bracket" | "underscore";

export interface PlaceholderHit {
  kind: PlaceholderKind;
  /** 命中的原文（下划线可能很长，展示时再截断）。 */
  text: string;
  /** 1 起的页码。 */
  page: number;
}

export interface PlaceholderScan {
  total: number;
  bracketCount: number;
  underscoreCount: number;
  /** 去掉重复后的前 5 个示例，带页码。 */
  examples: string[];
}

const BRACKET_RE = /【[^】\n]{0,40}】/g;
const UNDERSCORE_RE = /[_＿]{3,}/g;
const EXAMPLE_LIMIT = 5;
const EXAMPLE_TEXT_LIMIT = 24;

/** 单页文本里的占位符命中，按出现顺序返回。 */
export function findPlaceholdersInText(
  text: string,
  page: number,
): PlaceholderHit[] {
  const hits: PlaceholderHit[] = [];
  for (const match of text.matchAll(BRACKET_RE)) {
    hits.push({ kind: "bracket", text: match[0], page });
  }
  for (const match of text.matchAll(UNDERSCORE_RE)) {
    hits.push({ kind: "underscore", text: match[0], page });
  }
  return hits;
}

/** 汇总命中：数量按实际次数，示例按原文去重后取前 5 个。 */
export function summarizePlaceholders(hits: PlaceholderHit[]): PlaceholderScan {
  let bracketCount = 0;
  let underscoreCount = 0;
  const examples: string[] = [];
  const seen = new Set<string>();
  for (const hit of hits) {
    if (hit.kind === "bracket") {
      bracketCount += 1;
    } else {
      underscoreCount += 1;
    }
    if (seen.has(hit.text) || examples.length >= EXAMPLE_LIMIT) {
      continue;
    }
    seen.add(hit.text);
    examples.push(`第 ${hit.page} 页 ${displayText(hit.text)}`);
  }
  return {
    total: bracketCount + underscoreCount,
    bracketCount,
    underscoreCount,
    examples,
  };
}

function displayText(text: string): string {
  return text.length > EXAMPLE_TEXT_LIMIT
    ? `${text.slice(0, EXAMPLE_TEXT_LIMIT)}…`
    : text;
}

/**
 * 扫描整个模板 PDF。任一页取文失败都向上抛，由调用方转成「扫描失败」提示；
 * 失败不影响标定编辑器本身。
 */
export async function scanDocumentPlaceholders(
  doc: PdfDocument,
): Promise<PlaceholderScan> {
  const hits: PlaceholderHit[] = [];
  for (let page = 1; page <= doc.numPages; page += 1) {
    const pdfPage = await doc.getPage(page);
    const content = await pdfPage.getTextContent();
    const text = content.items.map((item) => item.str ?? "").join("");
    hits.push(...findPlaceholdersInText(text, page));
  }
  return summarizePlaceholders(hits);
}
