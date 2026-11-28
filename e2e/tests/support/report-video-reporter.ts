// 过程录像报告器：业务验收测试结束后，把 Playwright 录制的 WebM 复制到
// 报告目录（验收过程录像.webm），并把验收报告里的视频占位替换为内嵌播放器。
// 使用教程 HTML 直接以相对路径引用同名录像（business-tutorial-html.ts），
// 播放器自带加载/错误事件，无需在这里二次补丁。
//
// te-fp48w：复制之前先按测试留下的合成计划（报告目录 .pip/plan.json）把两段
// 手机侧录像以画中画合成进主录像（右下角、画面宽 1/3、绿色细边框），用户直接
// 看录像文件也能看到扫码之后手机上的录入过程。ffmpeg 不可用或合成失败时跳过
// 画中画、在报告视频区写明原因（步骤内嵌播放器不受影响），不影响 E2E 结论。
// 无论成功与否都在结束时删除 .pip 暂存目录，交付件仍是 4 个文件。
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
import { escapeHtml } from "./business-report-html";
import { TUTORIAL_VIDEO_NAME } from "./business-tutorial-html";
import {
  cleanupPipDir,
  compositePip,
  probeFfmpeg,
  readPipPlan,
} from "./business-video-pip";

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

interface PanelNotes {
  /** 画中画合成成功时的说明。 */
  note?: string;
  /** 画中画跳过/失败时的原因（不影响步骤内嵌录像）。 */
  warning?: string;
}

function playerBlock(videoName: string, notes: PanelNotes = {}): string {
  const note =
    notes.note === undefined
      ? ""
      : `\n    <p class="hint">${escapeHtml(notes.note)}</p>`;
  const warning =
    notes.warning === undefined
      ? ""
      : `\n    <p class="hint frame-warning">${escapeHtml(notes.warning)}</p>`;
  return `  <section class="panel video-panel">
    <h2>验收过程录像</h2>
    <video id="process-video" controls preload="metadata" src="${videoName}"></video>
    <p class="hint">整段业务过程已录制。点击每一步标题旁的「▶ 看本步过程（约 00:xx）」，视频会跳到该步开始的位置播放（误差约 1–3 秒）。</p>${note}${warning}
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

function errorSummary(error: unknown): string {
  if (error instanceof Error) {
    return error.message.split("\n")[0] ?? error.message;
  }
  return String(error);
}

/** 按测试留下的计划把手机侧录像合成进主录像；任何问题都只返回面板提示。 */
async function composePhonePip(
  videoPath: string,
  outputDir: string,
): Promise<PanelNotes> {
  try {
    const plan = await readPipPlan(outputDir);
    if (!plan || plan.segments.length === 0) {
      return {};
    }
    const ffmpeg = probeFfmpeg();
    if (!ffmpeg.path) {
      return {
        warning:
          `本次未把手机侧录像合成进主录像：${ffmpeg.reason ?? "ffmpeg 不可用"}。` +
          "步骤内的「手机侧实录」视频仍可正常播放，业务判定不受影响。",
      };
    }
    const result = await compositePip(
      ffmpeg.path,
      videoPath,
      plan.segments,
      outputDir,
    );
    if (result.warning) {
      return {
        warning: `${result.warning}。步骤内的「手机侧实录」视频仍可正常播放，业务判定不受影响。`,
      };
    }
    return {
      note:
        `手机侧操作（${result.applied} 段）已按所属步骤时间合成在录像右下角` +
        "（画面宽 1/3、绿色细边框）：直接看录像或在步骤里点「▶ 看本步过程」，" +
        "都能看到扫码之后手机上的录入过程。",
    };
  } catch (error) {
    return {
      warning:
        `手机侧画中画合成未完成（不影响报告与内嵌录像）：${errorSummary(error)}`,
    };
  }
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
    try {
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
      if (video?.path) {
        const videoName = this.options.videoName ?? TUTORIAL_VIDEO_NAME;
        const videoPath = join(outputDir, videoName);
        try {
          await copyFile(video.path, videoPath);
          // 复制完成后原位合成：失败时 compositePip 不碰主录像。
          const notes = await composePhonePip(videoPath, outputDir);
          block = playerBlock(videoName, notes);
          html = html.replace('data-video="0"', 'data-video="1"');
        } catch (error) {
          block = MISSING_VIDEO;
          console.warn(
            `业务验收录像复制失败（报告保留缺失说明）：${errorSummary(error)}`,
          );
        }
      }
      html = replaceBetween(html, VIDEO_START, VIDEO_END, block);
      await writeFile(reportPath, html, "utf8");
    } catch (error) {
      // 报告器异常不能顶掉测试结论：只记录警告。
      console.warn(`业务验收报告视频区补全失败：${errorSummary(error)}`);
    } finally {
      await cleanupPipDir(outputDir).catch(() => undefined);
    }
  }
}
