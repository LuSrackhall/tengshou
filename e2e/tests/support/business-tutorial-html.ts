// 使用教程的 HTML 渲染：自包含单文件（分镜 base64 内嵌），离线可打开；
// 过程视频为同目录相对引用（验收过程录像.webm，由 report-video-reporter.ts
// 在测试结束后复制到同目录）。播放器自带加载/错误事件：录像缺失时隐藏
// 播放器并显示说明，不会出现坏掉的播放器；有录像时才显示章节跳转按钮。
//
// 结构（对应已确认样张）：封面 → 目录 → 演示录像（可调播放速度）→
// 准备 + 每个业务步骤（你要做什么 / 你会看到什么 / 注意什么 + 分镜 +
// 视频章节跳转）→ 结尾（可打开示例最终 PDF）。
// 教程不渲染测试元信息：控制台检查步骤没有教程文案，报告自检与转换器
// API 状态卡也不进入教程。
import type { ReportFrameView } from "./business-report-html";
import { escapeHtml, formatClock } from "./business-report-html";

/** 与 report-video-reporter.ts 默认输出同名：同目录相对引用。 */
export const TUTORIAL_VIDEO_NAME = "验收过程录像.webm";

export interface TutorialStepView {
  /** 教程编号；准备步骤为「准备」，其余为 1..N。 */
  seq: string;
  title: string;
  do: string[];
  see: string[];
  note: string[];
  frames: ReportFrameView[];
  /** 相对录像起点的秒数，用于章节跳转。 */
  startedAtSeconds: number;
}

export interface TutorialModel {
  title: string;
  lead: string;
  version: string;
  /** true 表示本次为教程录制（已放慢并显示点击指示）。 */
  slowRecording: boolean;
  steps: TutorialStepView[];
  artifactLink: string;
  artifactName: string;
}

const VIDEO_NAME = TUTORIAL_VIDEO_NAME;

const CIRCLED = "①②③④⑤⑥⑦⑧⑨⑩⑪⑫⑬⑭⑮⑯⑰⑱⑲⑳";

function circled(index: number): string {
  return CIRCLED[index] ?? `(${index + 1})`;
}

function renderFrames(step: TutorialStepView): string {
  if (step.frames.length === 0) {
    return "";
  }
  const figures = step.frames
    .map((frame, index) => {
      const number = circled(index);
      const text = frame.caption.replace(
        /^[①②③④⑤⑥⑦⑧⑨⑩⑪⑫⑬⑭⑮⑯⑰⑱⑲⑳]+\s*/,
        "",
      );
      if (!frame.png) {
        return (
          `<figure class="frame missing"><figcaption>${number} ` +
          `${escapeHtml(text)}<br><span class="frame-warning">` +
          `截图采集失败：${escapeHtml(frame.warning ?? "未知原因")}` +
          `</span></figcaption></figure>`
        );
      }
      const kindClass =
        frame.kind === "pdf-page" ? "pdf" : frame.kind === "pdf-zoom" ? "zoom" : "";
      return (
        `<figure class="frame ${kindClass}">` +
        `<img alt="${escapeHtml(`${step.title} ${number} ${text}`)}" ` +
        `src="data:image/png;base64,${frame.png.toString("base64")}">` +
        `<figcaption>${number} ${escapeHtml(text)}</figcaption></figure>`
      );
    })
    .join("");
  return `<div class="frames">${figures}</div>`;
}

function renderTri(step: TutorialStepView): string {
  const items = (lines: string[], tag: "ol" | "ul") =>
    lines.length === 0
      ? ""
      : `<${tag}>${lines.map((line) => `<li>${escapeHtml(line)}</li>`).join("")}</${tag}>`;
  return (
    `<div class="tri">` +
    `<div class="tri-col do"><h3>你要做什么</h3>${items(step.do, "ol")}</div>` +
    `<div class="tri-col see"><h3>你会看到什么</h3>${items(step.see, "ul")}</div>` +
    `<div class="tri-col note"><h3>注意什么</h3>${items(step.note, "ul")}</div>` +
    `</div>`
  );
}

function renderStep(step: TutorialStepView, index: number): string {
  const seek =
    step.startedAtSeconds > 1
      ? `<span class="video-seek" data-start="${step.startedAtSeconds.toFixed(1)}">` +
        `▶ 看本步视频（${formatClock(step.startedAtSeconds)}）</span>`
      : "";
  return (
    `<section class="tut-step" id="s${index}">` +
    `<header><span class="seq">${escapeHtml(step.seq)}</span>` +
    `<h2>${escapeHtml(step.title)}</h2>${seek}</header>` +
    renderTri(step) +
    renderFrames(step) +
    `</section>`
  );
}

export function renderTutorialHtml(model: TutorialModel): string {
  const numbered = model.steps.filter((step) => step.seq !== "准备").length;
  const toc = model.steps
    .map((step, index) => {
      const label = step.seq === "准备" ? "准备" : `${step.seq}.`;
      return `<li><a href="#s${index}">${escapeHtml(label)} ${escapeHtml(step.title)}</a></li>`;
    })
    .join("");
  const records = model.slowRecording
    ? "录像已放慢操作节奏，并在点击处显示圆点与波纹；每步开头有 1 秒章节标题卡，可按需调整播放速度。"
    : "本教程由当前版本的真实运行自动生成；录像为原速运行过程，可按需调整播放速度。";
  const artifact = model.artifactLink
    ? `<p><a class="pdf-link" href="${escapeHtml(encodeURI(model.artifactLink))}" download>` +
      `打开本示例最终合同 PDF（${escapeHtml(model.artifactName)}）</a></p>`
    : "";
  return `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>${escapeHtml(model.title)}</title>
<style>
  :root { color-scheme: light; }
  * { box-sizing: border-box; }
  body { margin: 0; background: #f2f5f3; color: #1f2328;
    font: 16px/1.75 -apple-system, "PingFang SC", "Microsoft YaHei", "Noto Sans CJK SC", sans-serif; }
  main { max-width: 1040px; margin: 0 auto; padding: 28px 18px 60px; }
  h1 { font-size: 30px; margin: 0 0 12px; }
  h2 { font-size: 19px; margin: 0 0 6px; }
  h3 { font-size: 14px; margin: 0 0 6px; }
  .tut-cover { background: linear-gradient(160deg, #123c2b, #1f6f4a); color: #fff;
    border-radius: 16px; padding: 34px 30px; margin-bottom: 18px; }
  .tut-kicker { opacity: .85; letter-spacing: .12em; font-size: 13px; margin: 0 0 10px; }
  .tut-cover h1 { color: #fff; }
  .tut-lead { font-size: 16px; line-height: 1.8; margin: 0 0 14px; max-width: 52em; }
  .tut-meta { font-size: 13px; line-height: 1.7; opacity: .82; margin: 0; }
  .panel { background: #fff; border-radius: 14px; padding: 18px 20px;
    box-shadow: 0 8px 24px rgba(22,34,28,.08); margin-bottom: 18px; }
  .tut-toc ol { columns: 2; column-gap: 28px; line-height: 2; padding-left: 1.2em; margin: 6px 0 0; }
  .tut-toc a { color: #1f6f4a; text-decoration: none; }
  .video-panel video { width: 100%; max-height: 70vh; border-radius: 10px; background: #000; display: block; }
  .video-speed { margin: 8px 0 4px; font-size: 13px; color: #3d4642; }
  .video-speed button { margin-left: 6px; padding: 4px 12px; border: 1px solid #cfd6d1;
    border-radius: 999px; background: #fff; color: #3d4642; cursor: pointer; font-size: 13px; }
  .video-speed button.active { background: #1f6f4a; border-color: #1f6f4a; color: #fff; }
  .hint { color: #5b665f; font-size: 13.5px; margin: 8px 0 0; }
  .tut-step { background: #fff; border-radius: 14px; padding: 20px 22px 10px;
    margin-bottom: 16px; box-shadow: 0 6px 18px rgba(22,34,28,.06); border-top: 4px solid #1f6f4a; }
  .tut-step header { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
  .tut-step h2 { flex: 1 1 320px; }
  .tut-step .seq { display: inline-flex; min-width: 28px; height: 28px; padding: 0 8px;
    border-radius: 14px; background: #e3f1e9; color: #1f6f4a; align-items: center;
    justify-content: center; font-weight: 700; font-size: 13px; }
  .video-seek { display: none; align-items: center; gap: 4px; font-size: 13px;
    color: #1f6f4a; background: #e3f1e9; border: 1px solid #b9d9c7; border-radius: 999px;
    padding: 3px 10px; cursor: pointer; }
  body[data-video="1"] .video-seek { display: inline-flex; }
  .tri { display: grid; grid-template-columns: 1fr 1fr 1fr; gap: 14px; margin: 14px 0 6px; }
  .tri-col { border-radius: 10px; padding: 12px 14px; font-size: 14px; line-height: 1.75; }
  .tri-col ol, .tri-col ul { margin: 0; padding-left: 1.2em; }
  .tri-col.do { background: #f2f7f4; }
  .tri-col.do h3 { color: #1f6f4a; }
  .tri-col.see { background: #f4f6fb; }
  .tri-col.see h3 { color: #2c4a8a; }
  .tri-col.note { background: #fdf6ec; }
  .tri-col.note h3 { color: #8a5a00; }
  .frames { display: grid; gap: 14px; margin: 10px 0 14px; }
  figure.frame { margin: 0; }
  figure.frame img { width: 100%; height: auto; border: 1px solid #e2e7e4; border-radius: 8px;
    display: block; cursor: zoom-in; background: #fff; }
  figure.pdf img { max-width: 760px; margin: 0 auto; border-color: #c6d3cb; }
  figure.zoom img { max-width: 680px; margin: 0 auto; border-color: #e8bfbc; }
  figcaption { color: #46504b; font-size: 14.5px; padding: 6px 2px 0; }
  .frame.missing { background: #fff5f4; border: 1px dashed #e8bfbc; border-radius: 8px; padding: 10px 14px; }
  .frame-warning { color: #b4342c; font-size: 13.5px; }
  .tut-end { border-top-color: #cfd6d1; }
  .pdf-link { color: #1f6f4a; font-weight: 700; }
  footer { color: #7d857f; font-size: 13px; text-align: center; padding-top: 10px; }
  #lightbox { position: fixed; inset: 0; background: rgba(12,16,14,.84); display: flex;
    align-items: center; justify-content: center; padding: 24px; z-index: 50; cursor: zoom-out; }
  #lightbox[hidden] { display: none; }
  #lightbox img { max-width: 96vw; max-height: 92vh; border-radius: 6px; background: #fff;
    box-shadow: 0 20px 60px rgba(0,0,0,.5); }
  @media (max-width: 900px) { .tri { grid-template-columns: 1fr; } }
</style>
</head>
<body data-video="0">
<main>
  <section class="tut-cover">
    <p class="tut-kicker">腾手 · 用户教程</p>
    <h1>${escapeHtml(model.title)}</h1>
    <p class="tut-lead">${escapeHtml(model.lead)}</p>
    <p class="tut-meta">腾手版本 ${escapeHtml(model.version)} · 演示数据均为虚构（幸福村股份经济合作社、王建国等），仅用于说明操作。<br>${escapeHtml(records)}</p>
  </section>
  <section class="panel tut-toc">
    <h2>目录</h2>
    <ol>${toc}</ol>
  </section>
  <section class="panel video-panel">
    <h2>演示录像</h2>
    <video id="process-video" controls preload="metadata" src="${escapeHtml(encodeURI(VIDEO_NAME))}"></video>
    <p id="tutorial-video-missing" class="hint" hidden>本次运行没有录制过程视频；分镜与逐步讲解不受影响，仍可完整阅读整个流程。</p>
    <div class="video-speed">播放速度：
      <button type="button" data-rate="0.35">0.35×</button>
      <button type="button" data-rate="0.5">0.5×</button>
      <button type="button" data-rate="0.75">0.75×</button>
      <button type="button" data-rate="1" class="active">1× 原速</button>
    </div>
    <p class="hint">${escapeHtml(records)}</p>
  </section>
  ${model.steps.map((step, index) => renderStep(step, index)).join("")}
  <section class="tut-step tut-end">
    <h2>完成</h2>
    <p>走完这 ${numbered} 步，你就得到了一份签字、盖章齐全、可直接打印或发送的合同 PDF。下一份合同可以复用已建好的模板、字段和双方档案，只需要从「新建合同」开始。</p>
    ${artifact}
  </section>
  <footer>由腾手 E2E 自动生成 · 分镜已内嵌，教程可离线打开；过程视频为同目录相对引用</footer>
</main>
<div id="lightbox" hidden><img id="lightbox-img" alt="放大查看"></div>
<script>
(function () {
  var video = document.getElementById("process-video");
  var videoMissing = document.getElementById("tutorial-video-missing");
  function videoReady() { document.body.setAttribute("data-video", "1"); }
  function videoFailed() {
    if (video) { video.style.display = "none"; }
    if (videoMissing) { videoMissing.hidden = false; }
    document.body.setAttribute("data-video", "0");
  }
  if (video) {
    video.addEventListener("loadedmetadata", videoReady);
    video.addEventListener("error", videoFailed);
    if (video.readyState >= 1) { videoReady(); }
  } else {
    videoFailed();
  }
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
  var speedButtons = document.querySelectorAll(".video-speed button");
  function applyRate(rate) {
    if (video) { video.playbackRate = rate; }
    for (var j = 0; j < speedButtons.length; j += 1) {
      speedButtons[j].classList.toggle(
        "active",
        String(rate) === speedButtons[j].getAttribute("data-rate"),
      );
    }
  }
  if (video) {
    applyRate(1);
    for (var k = 0; k < speedButtons.length; k += 1) {
      speedButtons[k].addEventListener("click", function () {
        applyRate(Number(this.getAttribute("data-rate")));
      });
    }
  }
  var lightbox = document.getElementById("lightbox");
  var lightboxImg = document.getElementById("lightbox-img");
  if (lightbox && lightboxImg) {
    var figures = document.querySelectorAll("figure.frame img");
    for (var m = 0; m < figures.length; m += 1) {
      figures[m].addEventListener("click", function () {
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
