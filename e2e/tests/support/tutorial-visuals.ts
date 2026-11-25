// 使用教程的录像视觉层（仅 E2E_TUTORIAL=1 时启用）：
//   - 点击圆点 + 波纹：每次 pointerdown 在点击位置画一个实心圆点与扩散光环，
//     配色与产品内 agent 操作可见层一致（#5b54c9），让录像看得出点了哪里；
//   - 每步章节标题卡：步骤开始时整屏显示 1 秒「编号 + 教程标题」；
//   - 每步结束停顿：自然停顿，配合 slowMo 让操作节奏可读。
//
// 这些效果只存在于测试页面，不进产品代码；不加标题卡/圆点时业务流程不变。
import type { BrowserContext, Page } from "@playwright/test";

export function isTutorialMode(): boolean {
  return process.env.E2E_TUTORIAL === "1";
}

/**
 * 给 context 内所有页面（含手机签名页）注入点击圆点 + 波纹。
 * addInitScript 对后续创建的页面同样生效，因此必须在第一次跳转前调用。
 */
export async function installClickVisuals(
  context: BrowserContext,
): Promise<void> {
  await context.addInitScript(() => {
    const COLOR = "#5b54c9";
    const STYLE_ID = "e2e-tutorial-click-style";
    let styled = false;

    const ensureStyle = (): void => {
      if (styled || !document.head) {
        return;
      }
      styled = true;
      const style = document.createElement("style");
      style.id = STYLE_ID;
      style.textContent =
        "@keyframes e2e-tutorial-click-ripple{" +
        "from{opacity:.85;transform:scale(.4)}" +
        "to{opacity:0;transform:scale(2.6)}}";
      document.head.appendChild(style);
    };

    window.addEventListener(
      "pointerdown",
      (event: PointerEvent) => {
        if (event.button !== 0 || !document.body) {
          return;
        }
        ensureStyle();
        const host = document.createElement("div");
        host.dataset.e2eClick = "1";
        host.style.cssText =
          `position:fixed;left:${event.clientX}px;top:${event.clientY}px;` +
          "width:0;height:0;pointer-events:none;z-index:2147483647";
        const dot = document.createElement("span");
        dot.style.cssText =
          "position:absolute;left:-5px;top:-5px;width:10px;height:10px;" +
          `border-radius:50%;background:${COLOR};` +
          "box-shadow:0 0 0 3px rgba(91,84,201,.30)";
        const ripple = document.createElement("span");
        ripple.style.cssText =
          "position:absolute;left:-9px;top:-9px;width:18px;height:18px;" +
          `border-radius:50%;border:2px solid ${COLOR};` +
          "animation:e2e-tutorial-click-ripple .55s ease-out forwards";
        host.appendChild(ripple);
        host.appendChild(dot);
        document.body.appendChild(host);
        window.setTimeout(() => host.remove(), 700);
      },
      true,
    );
  });
}

/** 每步开头的章节标题卡：整屏显示 1 秒后移除。视觉层失败不影响业务步骤。 */
export async function showChapterCard(
  page: Page,
  seq: string,
  title: string,
): Promise<void> {
  const shown = await page
    .evaluate(
      ({ seqText, titleText }) => {
        const root = document.body ?? document.documentElement;
        if (!root) {
          return false;
        }
        const overlay = document.createElement("div");
        overlay.dataset.e2eChapter = "1";
        overlay.style.cssText =
          "position:fixed;inset:0;z-index:2147483646;display:flex;" +
          "flex-direction:column;align-items:center;justify-content:center;" +
          "gap:16px;background:linear-gradient(160deg,#123c2b,#1f6f4a);" +
          "color:#fff;pointer-events:none;" +
          'font-family:-apple-system,"PingFang SC","Microsoft YaHei",sans-serif';
        const badge = document.createElement("div");
        badge.textContent = seqText;
        badge.style.cssText =
          "min-width:56px;height:56px;padding:0 16px;border-radius:999px;" +
          "background:rgba(255,255,255,.16);text-align:center;" +
          "font:700 26px/56px -apple-system,'PingFang SC',sans-serif";
        const heading = document.createElement("div");
        heading.textContent = titleText;
        heading.style.cssText =
          "max-width:82vw;text-align:center;font-size:34px;font-weight:700;" +
          "line-height:1.4;letter-spacing:.02em";
        overlay.appendChild(badge);
        overlay.appendChild(heading);
        root.appendChild(overlay);
        return true;
      },
      { seqText: seq, titleText: title },
    )
    .catch(() => false);
  if (!shown) {
    return;
  }
  await page.waitForTimeout(1000);
  await page
    .evaluate(() => {
      for (const node of document.querySelectorAll("[data-e2e-chapter]")) {
        node.remove();
      }
    })
    .catch(() => undefined);
}

/** 每步结束后的自然停顿（约 1.2 秒），让录像里的结果停留可读。 */
export async function pauseAfterStep(page: Page, ms = 1200): Promise<void> {
  await page.waitForTimeout(ms);
}
