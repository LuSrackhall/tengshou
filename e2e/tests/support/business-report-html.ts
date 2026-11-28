// 业务验收报告的 HTML 渲染：自包含单文件（分镜 base64 内嵌），离线可打开；
// 过程视频是同目录相对引用，由 report-video-reporter.ts 在测试结束后复制并
// 把 <!--PROCESS_VIDEO_START-->…<!--PROCESS_VIDEO_END--> 替换成播放器。
//
// 报告结构：总判定 → 怎么看这份报告 → 过程录像 → 运行信息 → 报告自检 →
// 14 个业务步骤（预期 / 实际 / 状态卡 / ①②③ 分镜 + 图注 + 灯箱）→ 最终合同。
import type { FrameKind, StepCard } from "./business-frames";

export type Verdict = "符合预期" | "不符合预期";

export interface ReportMetaView {
  title: string;
  scenario: string;
  environment: string;
  startedAt: Date;
  appVersion: string;
}

export interface ReportFrameView {
  caption: string;
  png: Buffer | null;
  kind: FrameKind;
  warning?: string;
}

/** 步骤内嵌的手机侧页面录像：WebM base64 内联，离线可播放。 */
export interface ReportVideoView {
  /** 中文标签：这段录像是什么。 */
  label: string;
  webm: Buffer | null;
  /** 采集失败原因；webm 为 null 时给出。 */
  warning?: string;
}

export interface ReportStepView {
  action: string;
  expected: string;
  actual: string;
  verdict: Verdict;
  frames: ReportFrameView[];
  videos: ReportVideoView[];
  cards: StepCard[];
  /** 相对报告开始（≈ 视频起点）的秒数，用于跳转。 */
  startedAtSeconds: number;
}

export interface ReportArtifactView {
  name: string;
  details: string[];
}

export interface ReportModel {
  meta: ReportMetaView;
  steps: ReportStepView[];
  artifact?: ReportArtifactView;
  artifactLink: string;
  qualityIssues: string[];
  passedCount: number;
  failedCount: number;
}

const CIRCLED = "①②③④⑤⑥⑦⑧⑨⑩⑪⑫⑬⑭⑮⑯⑰⑱⑲⑳";

export function escapeHtml(value: string): string {
  return value
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");
}

export function stepNumber(index: number): string {
  return CIRCLED[index] ?? `(${index + 1})`;
}

export function formatClock(seconds: number): string {
  const total = Math.max(0, Math.round(seconds));
  const minutes = Math.floor(total / 60);
  return `${String(minutes).padStart(2, "0")}:${String(total % 60).padStart(2, "0")}`;
}

function formatTime(when: Date): string {
  return when.toLocaleString("zh-CN", { hour12: false });
}

function renderFrames(step: ReportStepView): string {
  if (step.frames.length === 0) {
    return "";
  }
  const figures = step.frames
    .map((frame, index) => {
      const number = stepNumber(index);
      // 图注本身可能已带「①」等前缀（分镜清单写法），渲染时统一由报告编号，
      // 避免出现「① ① ...」。
      const text = frame.caption.replace(
        /^[①②③④⑤⑥⑦⑧⑨⑩⑪⑫⑬⑭⑮⑯⑰⑱⑲⑳]+\s*/,
        "",
      );
      const caption = `${number} ${escapeHtml(text)}`;
      if (!frame.png) {
        return (
          `<figure class="frame missing">` +
          `<figcaption>${caption}<br>` +
          `<span class="frame-warning">截图采集失败：${escapeHtml(frame.warning ?? "未知原因")}</span>` +
          `</figcaption></figure>`
        );
      }
      const kindClass =
        frame.kind === "pdf-page" ? "pdf" : frame.kind === "pdf-zoom" ? "zoom" : "";
      return (
        `<figure class="frame ${kindClass}">` +
        `<img alt="${escapeHtml(`${step.action} ${number} ${text}`)}" ` +
        `src="data:image/png;base64,${frame.png.toString("base64")}">` +
        `<figcaption>${caption}</figcaption></figure>`
      );
    })
    .join("");
  return `<div class="frames">${figures}</div>`;
}

function renderVideos(step: ReportStepView): string {
  if (step.videos.length === 0) {
    return "";
  }
  const blocks = step.videos
    .map((video) => {
      if (!video.webm) {
        return (
          `<div class="step-video missing">` +
          `<p class="video-label">${escapeHtml(video.label)}</p>` +
          `<p class="frame-warning">手机侧录像缺失：${escapeHtml(video.warning ?? "未知原因")}</p>` +
          `</div>`
        );
      }
      return (
        `<figure class="step-video">` +
        `<video controls preload="metadata" ` +
        `src="data:video/webm;base64,${video.webm.toString("base64")}"></video>` +
        `<figcaption>${escapeHtml(video.label)}</figcaption></figure>`
      );
    })
    .join("");
  // 放在步骤头部、分镜之前：用户点「▶ 看本步过程」跳主录像时间点，或直接
  // 看这里的内嵌录像，都能看到扫码之后手机上发生的完整录入过程。
  return (
    `<div class="step-videos">` +
    `<h3 class="step-videos-title">手机侧实录（扫码后手机上的完整录入过程）</h3>` +
    blocks +
    `</div>`
  );
}

function renderCards(step: ReportStepView): string {
  return step.cards
    .map(
      (card) =>
        `<div class="status-card ${card.tone}">` +
        `<h3>${escapeHtml(card.title)}</h3>` +
        `<ul>${card.lines.map((line) => `<li>${escapeHtml(line)}</li>`).join("")}</ul>` +
        `</div>`,
    )
    .join("");
}

function renderStep(step: ReportStepView, index: number): string {
  const badge = step.verdict === "符合预期" ? "pass" : "fail";
  const seek = formatClock(step.startedAtSeconds);
  return (
    `<section class="step ${badge}">` +
    `<header><span class="seq">${index + 1}</span>` +
    `<h2>${escapeHtml(step.action)}</h2>` +
    `<span class="video-seek" data-start="${step.startedAtSeconds.toFixed(1)}">` +
    `▶ 看本步过程（约 ${seek}）</span>` +
    `<span class="badge ${badge}">${escapeHtml(step.verdict)}</span></header>` +
    `<dl><dt>预期结果</dt><dd>${escapeHtml(step.expected)}</dd>` +
    `<dt>实际结果</dt><dd>${escapeHtml(step.actual)}</dd></dl>` +
    renderVideos(step) +
    renderFrames(step) +
    renderCards(step) +
    `</section>`
  );
}

export function renderReportHtml(model: ReportModel): string {
  const { meta } = model;
  const total = model.steps.length;
  const verdict =
    model.failedCount === 0
      ? "全部步骤符合预期"
      : `有 ${model.failedCount} 个步骤未达到预期`;
  const summaryClass = model.failedCount === 0 ? "pass" : "fail";
  const quality =
    model.qualityIssues.length === 0
      ? `<p class="quality-ok">报告自检通过：交互步骤都有 ≥2 张分镜与中文图注；最终合同 PDF 三页渲染与签名/公章放大图齐全。</p>`
      : `<ul>${model.qualityIssues.map((issue) => `<li>${escapeHtml(issue)}</li>`).join("")}</ul>`;
  const artifact = model.artifact
    ? `<section class="artifact"><h2>最终合同</h2>` +
      `<p><a href="${encodeURI(model.artifactLink)}" download>下载最终合同 PDF：${escapeHtml(model.artifact.name)}</a></p>` +
      `<ul>${model.artifact.details.map((item) => `<li>${escapeHtml(item)}</li>`).join("")}</ul></section>`
    : "";
  return `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>${escapeHtml(meta.title)}</title>
<style>
  :root { color-scheme: light; }
  * { box-sizing: border-box; }
  body { margin: 0; background: #f2f5f3; color: #1f2328;
    font: 16px/1.7 -apple-system, "PingFang SC", "Microsoft YaHei", "Noto Sans CJK SC", sans-serif; }
  main { max-width: 1040px; margin: 0 auto; padding: 28px 18px 60px; }
  h1 { font-size: 26px; margin: 0 0 6px; }
  h2 { font-size: 20px; margin: 0 0 10px; }
  h3 { font-size: 15.5px; margin: 0 0 4px; }
  .lede { color: #5f6670; margin: 0 0 18px; }
  .panel { background: #fff; border-radius: 14px; padding: 18px 20px; box-shadow: 0 8px 24px rgba(22,34,28,.08); margin-bottom: 18px; }
  .verdict { font-size: 20px; font-weight: 700; margin: 0 0 6px; }
  .verdict.pass { color: #1f6f4a; }
  .verdict.fail { color: #b4342c; }
  .disclaimer { border-left: 4px solid #d9a400; background: #fff8e6; padding: 10px 14px; border-radius: 8px; }
  table.meta { width: 100%; border-collapse: collapse; font-size: 14.5px; }
  table.meta th { text-align: left; color: #5f6670; font-weight: 500; width: 110px; padding: 3px 10px 3px 0; }
  table.meta td { padding: 3px 0; word-break: break-all; }
  .howto ul, .quality ul, .artifact ul, .status-card ul { margin: 6px 0 0; padding-left: 22px; }
  .howto li { margin: 2px 0; }
  .quality-ok { color: #1f6f4a; margin: 0; }
  .quality .fail-note { color: #8a5a00; margin: 0 0 6px; }
  .video-panel video { width: 100%; max-height: 70vh; border-radius: 10px; background: #000; display: block; }
  .step { background: #fff; border-radius: 14px; padding: 16px 20px 6px; margin-bottom: 16px; box-shadow: 0 6px 18px rgba(22,34,28,.06); border-top: 4px solid #cfd6d1; }
  .step.pass { border-top-color: #1f6f4a; }
  .step.fail { border-top-color: #b4342c; }
  .step header { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
  .step h2 { font-size: 18px; margin: 0; flex: 1 1 340px; }
  .seq { display: inline-flex; width: 28px; height: 28px; border-radius: 50%; background: #eef1ef; color: #3d4642; align-items: center; justify-content: center; font-weight: 700; font-size: 14px; }
  .badge { font-size: 13px; padding: 3px 10px; border-radius: 999px; white-space: nowrap; }
  .badge.pass { background: #e3f1e9; color: #1f6f4a; }
  .badge.fail { background: #fbe7e5; color: #b4342c; }
  .video-seek { display: none; align-items: center; gap: 4px; font-size: 13px; color: #1f6f4a; background: #e3f1e9; border: 1px solid #b9d9c7; border-radius: 999px; padding: 3px 10px; cursor: pointer; }
  body[data-video="1"] .video-seek { display: inline-flex; }
  dl { display: grid; grid-template-columns: 90px 1fr; gap: 4px 12px; margin: 12px 0; }
  dt { color: #5f6670; font-size: 14px; }
  dd { margin: 0; }
  .status-card { background: #f6f9f7; border: 1px solid #e2e7e4; border-radius: 10px; padding: 10px 14px; margin: 10px 0 14px; }
  .status-card.pass { background: #eef7f1; border-color: #cfe6d8; }
  .status-card ul { font-size: 14.5px; }
  .frames { display: grid; gap: 14px; margin: 10px 0 14px; }
  .step-videos { display: grid; gap: 14px; margin: 10px 0 14px; }
  .step-videos-title { margin: 2px 0 0; font-size: 15.5px; color: #1f2328; }
  figure.step-video { margin: 0; }
  figure.step-video video { width: 100%; max-height: 70vh; border: 1px solid #e2e7e4; border-radius: 10px; background: #000; display: block; }
  figure.frame { margin: 0; }
  figure.frame img { width: 100%; height: auto; border: 1px solid #e2e7e4; border-radius: 8px; display: block; cursor: zoom-in; background: #fff; }
  figure.pdf img { max-width: 760px; margin: 0 auto; border-color: #c6d3cb; }
  figure.zoom img { max-width: 680px; margin: 0 auto; border-color: #e8bfbc; }
  figcaption { color: #46504b; font-size: 14.5px; padding: 6px 2px 0; }
  .frame.missing { background: #fff5f4; border: 1px dashed #e8bfbc; border-radius: 8px; padding: 10px 14px; }
  .step-video.missing { background: #fff5f4; border: 1px dashed #e8bfbc; border-radius: 8px; padding: 10px 14px; }
  .step-video.missing .video-label { margin: 0 0 4px; color: #46504b; font-size: 14.5px; font-weight: 600; }
  .frame-warning { color: #b4342c; font-size: 13.5px; }
  .artifact { background: #fff; border-radius: 14px; padding: 18px 20px; box-shadow: 0 8px 24px rgba(22,34,28,.08); margin-bottom: 18px; }
  .artifact a { color: #1f6f4a; font-weight: 700; }
  footer { color: #7d857f; font-size: 13px; text-align: center; padding-top: 10px; }
  #lightbox { position: fixed; inset: 0; background: rgba(12,16,14,.84); display: flex; align-items: center; justify-content: center; padding: 24px; z-index: 50; cursor: zoom-out; }
  #lightbox[hidden] { display: none; }
  #lightbox img { max-width: 96vw; max-height: 92vh; border-radius: 6px; background: #fff; box-shadow: 0 20px 60px rgba(0,0,0,.5); }
  @media (max-width: 640px) { dl { grid-template-columns: 1fr; } dt { margin-top: 6px; } }
</style>
</head>
<body data-video="0">
<main>
  <h1>${escapeHtml(meta.title)}</h1>
  <p class="lede">${escapeHtml(meta.scenario)}</p>
  <section class="panel">
    <p class="verdict ${summaryClass}">${escapeHtml(verdict)}（共 ${total} 步：符合预期 ${model.passedCount} 步）</p>
    <div class="disclaimer">本合同中的单位、姓名、证件号、电话、地址、金额与公章编号全部虚构，仅用于演示与测试，不具有任何法律效力。</div>
  </section>
  <section class="panel howto">
    <h2>怎么看这份报告</h2>
    <ul>
      <li>每一步按顺序展示：业务动作 → 预期结果 → 实际结果 → 手机侧实录（如有）→ 过程分镜 → 判定。</li>
      <li>过程分镜按真实发生顺序编号（①②③…），每张图下的文字说明「做了什么、界面发生了什么、看图重点」。</li>
      <li>分镜里的<strong style="color:#b4342c">红框</strong>是这一步的重点元素；图片已裁剪到相关区域，点击图片可放大到原始分辨率。</li>
      <li>「手机侧实录（扫码后手机上的完整录入过程）」是手机页本身的完整录像；若本次运行合成了画中画，主录像在对应时间段右下角也会同步显示手机画面。</li>
      <li>若本次运行录制了过程视频，每步标题旁的「▶ 看本步过程」会跳到视频中该步开始的位置。</li>
      <li>最后一步把最终合同 PDF 逐页渲染进报告，并在关键值、签名与公章处画红框。</li>
    </ul>
  </section>
<!--PROCESS_VIDEO_START-->
  <section class="panel video-panel">
    <h2>验收过程录像</h2>
    <p class="hint">本次运行没有交付过程视频（视频文件缺失）；分镜与 PDF 页面不受影响，仍可完整查看整个业务过程。</p>
  </section>
<!--PROCESS_VIDEO_END-->
  <section class="panel">
    <table class="meta">
      <tr><th>开始时间</th><td>${escapeHtml(formatTime(meta.startedAt))}</td></tr>
      <tr><th>运行环境</th><td>${escapeHtml(meta.environment)}</td></tr>
      <tr><th>腾手版本</th><td>${escapeHtml(meta.appVersion)}</td></tr>
      <tr><th>测试形态</th><td>发布形态 tengshou-cli serve + go:embed 界面，离线运行</td></tr>
    </table>
  </section>
  <section class="panel quality ${model.qualityIssues.length === 0 ? "pass" : "fail"}">
    <h2>报告自检</h2>
    ${model.qualityIssues.length === 0 ? "" : `<p class="fail-note">以下问题只影响报告呈现，不改变业务判定：</p>`}
    ${quality}
  </section>
  ${model.steps.map((step, index) => renderStep(step, index)).join("")}
  ${artifact}
  <footer>由腾手 E2E 自动生成 · 分镜已内嵌，报告可离线打开；过程视频为同目录相对引用</footer>
</main>
<div id="lightbox" hidden><img id="lightbox-img" alt="放大查看"></div>
<script>
(function () {
  var video = document.getElementById("process-video");
  var buttons = document.querySelectorAll(".video-seek");
  for (var i = 0; i < buttons.length; i += 1) {
    buttons[i].addEventListener("click", function () {
      if (!video) { return; }
      var start = Number(this.getAttribute("data-start") || "0");
      video.currentTime = Math.max(0, start);
      var played = video.play();
      if (played && played.catch) { played.catch(function () {}); }
      video.scrollIntoView({ behavior: "smooth", block: "center" });
    });
  }
  var lightbox = document.getElementById("lightbox");
  var lightboxImg = document.getElementById("lightbox-img");
  if (lightbox && lightboxImg) {
    var figures = document.querySelectorAll("figure.frame img");
    for (var j = 0; j < figures.length; j += 1) {
      figures[j].addEventListener("click", function () {
        lightboxImg.src = this.src;
        lightboxImg.alt = this.alt;
        lightbox.hidden = false;
      });
    }
    lightbox.addEventListener("click", function () { lightbox.hidden = true; });
    document.addEventListener("keydown", function (event) {
      if (event.key === "Escape") { lightbox.hidden = true; }
    });
  }
})();
</script>
</body>
</html>
`;
}
