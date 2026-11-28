// 业务验收报告的分镜采集：在目标元素上注入红色高亮框 + 编号徽标，按目标
// 区域裁剪截图并配中文图注。截图失败只记录警告（不抛错），报告里显示
// 「截图采集失败：原因」，业务断言与判定不受影响。
//
// 裁剪方式：先截当前视口，再在页面内用 canvas 按目标包围盒裁剪。这样不依赖
// Playwright clip 的文档坐标语义，三端（Chromium / WebKit）行为一致；裁剪
// 宽度按截图实际像素与 CSS 视口宽度换算，兼容 deviceScaleFactor=2。
import { mkdir, readFile, rm } from "node:fs/promises";
import { join } from "node:path";
import type { Locator, Page } from "@playwright/test";
import { PIP_DIR_NAME } from "./business-video-pip";

export type FrameKind = "page" | "screenshot" | "pdf-page" | "pdf-zoom" | "image";

export interface FrameRecord {
  /** 中文图注：做了什么 → 界面发生了什么 → 红框看哪里。 */
  caption: string;
  png: Buffer | null;
  kind: FrameKind;
  /** 采集失败原因；png 为 null 时给出。 */
  warning?: string;
}

/** 步骤内的页面录像（手机侧），以 WebM Buffer 内嵌进报告 HTML。 */
export interface VideoRecord {
  /** 中文标签：这段录像是什么、能看清什么。 */
  label: string;
  /** 录像内容；采集失败为 null。 */
  webm: Buffer | null;
  /** 采集失败原因；webm 为 null 时给出。 */
  warning?: string;
  /** 手机页创建时刻相对报告起点的秒数（主录像画中画窗口起点）。 */
  startedAtSeconds?: number;
  /** 手机页从创建到关闭的墙钟时长（秒，画中画窗口长度）。 */
  durationSeconds?: number;
  /** 手机侧录像落盘路径（报告目录 .pip 下），供报告器合成进主录像。 */
  segmentPath?: string;
}

export interface StepCard {
  title: string;
  lines: string[];
  tone: "info" | "pass";
}

interface Box {
  x: number;
  y: number;
  width: number;
  height: number;
}

export interface CaptureOptions {
  /** 红框高亮的目标元素；多个目标按 1..N 编号。 */
  targets?: Locator[];
  /** 裁剪区域；缺省为全部目标的并集；给出时区域裁剪、目标仍高亮。 */
  region?: Locator;
  /** 在另一页面截图（例如手机签名页）。 */
  page?: Page;
  /** 整页截图（不裁剪、不加红框）。 */
  wholePage?: boolean;
  /** 裁剪留白（CSS 像素）。 */
  padding?: number;
}

/** `.pip` 下手机侧录像的模块级序号：跨 FrameRecorder 实例递增，避免同名覆盖。 */
let phoneVideoSeq = 0;

/** 下一段手机侧录像的暂存路径（模块级序号，跨 FrameRecorder 实例唯一）。 */
export function nextPhoneVideoPath(pipDir: string): string {
  phoneVideoSeq += 1;
  return join(pipDir, `phone-${process.pid}-${phoneVideoSeq}.webm`);
}

/** 一步业务动作的采集器：分镜 + 状态卡 + 手机侧页面录像。 */
export class FrameRecorder {
  readonly frames: FrameRecord[] = [];
  readonly cards: StepCard[] = [];
  readonly videos: VideoRecord[] = [];

  constructor(
    private readonly page: Page,
    /**
     * 报告输出目录：手机侧录像 saveAs 到这里的隐藏 `.pip` 暂存目录，
     * 读入 Buffer 内嵌 HTML；暂存文件留给报告器合成画中画后统一删除。
     */
    private readonly videoDir = "",
    /** 报告起点（与步骤 startedAtSeconds 同一时钟）。 */
    private readonly clockStart = Date.now(),
  ) {}

  /** 采集一张分镜；失败时记录警告，不中断业务流程。 */
  async capture(caption: string, options: CaptureOptions = {}): Promise<void> {
    const targetPage = options.page ?? this.page;
    try {
      if (options.wholePage) {
        const png = await targetPage.screenshot({ fullPage: true });
        this.frames.push({ caption, png, kind: "page" });
        return;
      }
      const png = await captureRegion(targetPage, options);
      this.frames.push({ caption, png, kind: "screenshot" });
    } catch (error) {
      this.frames.push({
        caption,
        png: null,
        kind: "screenshot",
        warning: errorSummary(error),
      });
    }
  }

  /**
   * 采集另一个页面（手机侧）的完整录像。Playwright 对 context 内每个 page
   * 各录一份，但只有 fixture 页面的录像会作为附件交给报告器；手机页录像
   * 必须在 page.close() 之后用 video().saveAs() 取回（关闭前不保证落盘）。
   * 录像读入 Buffer 内嵌进报告/教程；暂存文件留在 `.pip` 下，由报告器按
   * startedAtMs 换算的画中画窗口合成进主录像后统一删除。录像不可用时只记
   * 警告，业务步骤照常判定。
   *
   * @param startedAtMs 手机页创建（开始录制）时刻；默认取调用时刻。
   */
  async capturePageVideo(
    page: Page,
    label: string,
    startedAtMs?: number,
  ): Promise<void> {
    const startedAt = startedAtMs ?? Date.now();
    try {
      const video = page.video();
      if (!video) {
        throw new Error("该页面未启用录像（video 为 null）");
      }
      if (!this.videoDir) {
        throw new Error("未提供报告输出目录，无法保存手机侧录像");
      }
      const pipDir = join(this.videoDir, PIP_DIR_NAME);
      await mkdir(pipDir, { recursive: true });
      // 每个业务步骤各用一个 FrameRecorder（runStep 新建），序号必须是
      // 模块级才能保证两段手机录像不互相覆盖：第二步的 saveAs 若同名会把
      // 第一步的暂存文件覆盖，画中画两段就都指向最后一段的录像。
      const tempPath = nextPhoneVideoPath(pipDir);
      try {
        await video.saveAs(tempPath);
        const webm = await readFile(tempPath);
        this.videos.push({
          label,
          webm,
          startedAtSeconds: Math.max(0, (startedAt - this.clockStart) / 1000),
          durationSeconds: Math.max(0, (Date.now() - startedAt) / 1000),
          segmentPath: tempPath,
        });
      } catch (error) {
        await rm(tempPath, { force: true }).catch(() => undefined);
        throw error;
      }
    } catch (error) {
      this.videos.push({ label, webm: null, warning: errorSummary(error) });
    }
  }

  /** 加入一张已渲染好的图片（最终 PDF 页面 / 放大图）。 */
  addImage(caption: string, png: Buffer, kind: FrameKind = "image"): void {
    this.frames.push({ caption, png, kind });
  }

  /** 记录不配截图的状态卡（环境检查、文本断言、控制台检查）。 */
  card(card: StepCard): void {
    this.cards.push(card);
  }
}

async function captureRegion(page: Page, options: CaptureOptions): Promise<Buffer> {
  const targets = options.targets ?? [];
  const scrollTarget = options.region ?? targets[0];
  if (scrollTarget) {
    await scrollTarget.scrollIntoViewIfNeeded({ timeout: 5_000 }).catch(() => undefined);
  }
  if (options.region && targets.length > 0) {
    // 页面级区域（例如整页 PDF）可能高于视口：把目标框滚进视口，保证
    // 红框与图注描述的位置确实在裁剪图里。
    await targets[0]!
      .scrollIntoViewIfNeeded({ timeout: 5_000 })
      .catch(() => undefined);
  }
  const targetBoxes: Box[] = [];
  for (const target of targets) {
    const box = await target.boundingBox();
    if (box) {
      targetBoxes.push(box);
    }
  }
  const regionBox = options.region
    ? await options.region.boundingBox()
    : null;
  const viewport = page.viewportSize() ?? { width: 1440, height: 960 };
  let crop = regionBox ?? unionBoxes(targetBoxes);
  if (!crop) {
    // 目标不可见：退回当前视口截图，至少保留界面上下文。
    return await page.screenshot();
  }
  const targetUnion = unionBoxes(targetBoxes);
  if (!regionBox && targetUnion) {
    // 单个按钮 / 表格行往往只有几十像素：扩到至少 520×360，保留可读的
    // 界面上下文（工具栏、相邻行、PDF 页局部）。
    crop = expandBox(targetUnion, 520, 360);
  }
  if (
    regionBox &&
    targetUnion &&
    (regionBox.height > viewport.height * 0.9 ||
      regionBox.width > viewport.width * 0.95)
  ) {
    // 大区域（整页 PDF / 长列表）+ 明确目标：裁到目标附近，避免只截到
    // 区域顶部而错过红框。
    crop = expandBox(targetUnion, 520, 360);
  }
  const padding = options.padding ?? 16;
  const clip = clampBox(
    {
      x: crop.x - padding,
      y: crop.y - padding,
      width: crop.width + padding * 2,
      height: crop.height + padding * 2,
    },
    viewport,
  );
  await injectHighlights(page, targetBoxes);
  let shot: Buffer;
  try {
    await page.waitForTimeout(60); // 让红框完成一次绘制
    shot = await page.screenshot();
  } finally {
    await clearHighlights(page);
  }
  return await cropShot(page, shot, clip);
}

/** 在页面内把视口截图裁剪到 clip 区域（CSS 像素）。 */
async function cropShot(page: Page, shot: Buffer, clip: Box): Promise<Buffer> {
  const dataUrl = await page.evaluate(
    async ({ png, rect }) => {
      const image = new Image();
      image.src = `data:image/png;base64,${png}`;
      await image.decode();
      const scaleX = image.naturalWidth / window.innerWidth;
      const scaleY = image.naturalHeight / window.innerHeight;
      const canvas = document.createElement("canvas");
      canvas.width = Math.max(1, Math.round(rect.width * scaleX));
      canvas.height = Math.max(1, Math.round(rect.height * scaleY));
      const context = canvas.getContext("2d");
      if (!context) {
        throw new Error("无法创建裁剪画布");
      }
      context.drawImage(
        image,
        Math.round(rect.x * scaleX),
        Math.round(rect.y * scaleY),
        canvas.width,
        canvas.height,
        0,
        0,
        canvas.width,
        canvas.height,
      );
      return canvas.toDataURL("image/png");
    },
    { png: shot.toString("base64"), rect: clip },
  );
  const comma = dataUrl.indexOf(",");
  return Buffer.from(comma >= 0 ? dataUrl.slice(comma + 1) : "", "base64");
}

async function injectHighlights(page: Page, boxes: Box[]): Promise<void> {
  if (boxes.length === 0) {
    return;
  }
  await page.evaluate((rects) => {
    const host = document.createElement("div");
    host.dataset.e2eHighlight = "1";
    host.style.cssText =
      "position:fixed;left:0;top:0;width:0;height:0;z-index:2147483647;pointer-events:none";
    rects.forEach((rect, index) => {
      const outline = document.createElement("div");
      outline.style.cssText =
        `position:fixed;left:${rect.x - 3}px;top:${rect.y - 3}px;` +
        `width:${rect.width + 6}px;height:${rect.height + 6}px;` +
        "border:3px solid #e02424;border-radius:6px;box-sizing:content-box;" +
        "box-shadow:0 0 0 2px rgba(224,36,36,.28);pointer-events:none";
      host.appendChild(outline);
      if (rects.length > 1) {
        const badge = document.createElement("div");
        badge.textContent = String(index + 1);
        badge.style.cssText =
          `position:fixed;left:${rect.x - 12}px;top:${rect.y - 12}px;` +
          "min-width:22px;height:22px;padding:0 4px;border-radius:11px;" +
          "background:#e02424;color:#fff;font:700 13px/22px system-ui,sans-serif;" +
          "text-align:center;box-sizing:border-box;pointer-events:none";
        host.appendChild(badge);
      }
    });
    document.body.appendChild(host);
  }, boxes);
}

async function clearHighlights(page: Page): Promise<void> {
  await page
    .evaluate(() => {
      for (const node of document.querySelectorAll("[data-e2e-highlight]")) {
        node.remove();
      }
    })
    .catch(() => undefined);
}

function unionBoxes(boxes: Box[]): Box | null {
  if (boxes.length === 0) {
    return null;
  }
  let left = Number.POSITIVE_INFINITY;
  let top = Number.POSITIVE_INFINITY;
  let right = Number.NEGATIVE_INFINITY;
  let bottom = Number.NEGATIVE_INFINITY;
  for (const box of boxes) {
    left = Math.min(left, box.x);
    top = Math.min(top, box.y);
    right = Math.max(right, box.x + box.width);
    bottom = Math.max(bottom, box.y + box.height);
  }
  return { x: left, y: top, width: right - left, height: bottom - top };
}

function clampBox(box: Box, viewport: { width: number; height: number }): Box {
  const x = Math.max(0, Math.min(box.x, viewport.width - 1));
  const y = Math.max(0, Math.min(box.y, viewport.height - 1));
  const right = Math.min(viewport.width, Math.max(box.x + box.width, x + 1));
  const bottom = Math.min(viewport.height, Math.max(box.y + box.height, y + 1));
  return { x, y, width: right - x, height: bottom - y };
}

/** 把目标框扩到至少 minWidth × minHeight（居中），让裁剪图保留上下文。 */
function expandBox(box: Box, minWidth: number, minHeight: number): Box {
  const width = Math.max(box.width, minWidth);
  const height = Math.max(box.height, minHeight);
  return {
    x: box.x - (width - box.width) / 2,
    y: box.y - (height - box.height) / 2,
    width,
    height,
  };
}

function errorSummary(error: unknown): string {
  if (error instanceof Error) {
    return error.message.split("\n")[0] ?? error.message;
  }
  return String(error);
}
