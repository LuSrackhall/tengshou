// 过程录像报告器：业务验收测试结束后，把 Playwright 录制的 WebM 复制到
// 报告目录（验收过程录像.webm），并把验收报告里的视频占位替换为内嵌播放器。
// 使用教程 HTML 直接以相对路径引用同名录像（business-tutorial-html.ts），
// 播放器自带加载/错误事件，无需在这里二次补丁。
//
// 视频在测试结束（testInfo 结果确定）后才落盘，因此报告正文先写占位注释，
// 由本报告器在 onTestEnd 里补全；找不到视频时保留「未录制到视频」说明，
// 不会出现坏掉的播放器。
import { copyFile, readFile, writeFile } from "node:fs/promises";
import { dirname, join, resolve } from "node:path";
import type {
  FullConfig,
  Reporter,
  TestCase,
  TestResult,
} from "@playwright/test/reporter";
import { TUTORIAL_VIDEO_NAME } from "./business-tutorial-html";

const VIDEO_START = "<!--PROCESS_VIDEO_START-->";
const VIDEO_END = "<!--PROCESS_VIDEO_END-->";

interface ReporterOptions {
  /** 相对 Playwright rootDir 的报告目录。 */
  outputDir?: string;
  reportName?: string;
  videoName?: string;
}

const MISSING_VIDEO = `  <section class="panel video-panel">
    <h2>验收过程录像</h2>
    <p class="hint">本次运行没有交付过程视频（视频文件缺失）；分镜与 PDF 页面不受影响，仍可完整查看整个业务过程。</p>
  </section>`;

function playerBlock(videoName: string): string {
  return `  <section class="panel video-panel">
    <h2>验收过程录像</h2>
    <video id="process-video" controls preload="metadata" src="${videoName}"></video>
    <p class="hint">整段业务过程已录制。点击每一步标题旁的「▶ 看本步过程（约 00:xx）」，视频会跳到该步开始的位置播放（误差约 1–3 秒）。</p>
  </section>`;
}

export function replaceBetween(
  source: string,
  start: string,
  end: string,
  replacement: string,
): string {
  const from = source.indexOf(start);
  const to = source.indexOf(end, from + start.length);
  if (from < 0 || to < 0) {
    return source;
  }
  return source.slice(0, from) + replacement + source.slice(to + end.length);
}

export default class ProcessVideoReporter implements Reporter {
  private rootDir = process.cwd();

  constructor(private readonly options: ReporterOptions = {}) {}

  onBegin(config: FullConfig): void {
    // FullConfig.rootDir 是 testDir（e2e/tests），报告目录在 e2e/ 下，
    // 因此优先按配置文件所在目录解析相对路径。
    this.rootDir = config.configFile
      ? dirname(config.configFile)
      : config.rootDir;
  }

  async onTestEnd(test: TestCase, result: TestResult): Promise<void> {
    if (!test.location.file.includes("business-acceptance.spec")) {
      return;
    }
    const outputDir = resolve(
      this.rootDir,
      this.options.outputDir ?? "test-results/business-acceptance",
    );
    const reportPath = join(
      outputDir,
      this.options.reportName ?? "业务验收报告.html",
    );
    let html: string;
    try {
      html = await readFile(reportPath, "utf8");
    } catch {
      return; // 用例未运行（收集阶段跳过）或报告尚未生成
    }
    const video = [...result.attachments]
      .reverse()
      .find(
        (attachment) =>
          attachment.path &&
          (attachment.name === "video" ||
            attachment.contentType?.startsWith("video/")),
      );
    let block = MISSING_VIDEO;
    let videoReady = false;
    if (video?.path) {
      const videoName = this.options.videoName ?? TUTORIAL_VIDEO_NAME;
      const videoPath = join(outputDir, videoName);
      try {
        await copyFile(video.path, videoPath);
        block = playerBlock(videoName);
        videoReady = true;
      } catch {
        block = MISSING_VIDEO;
      }
    }
    html = replaceBetween(html, VIDEO_START, VIDEO_END, block);
    if (videoReady) {
      html = html.replace('data-video="0"', 'data-video="1"');
    }
    await writeFile(reportPath, html, "utf8");
  }
}
