// 用户可读的「业务验收报告」：把 E2E 的业务动作、预期、实际、判定与过程
// 分镜（每步多张带图注的局部截图）渲染成自包含单文件 HTML（图片 base64
// 内嵌、离线可打开），并复制最终合同 PDF。
//
// 失败也会写入报告：失败步骤标红、写明实际结果与分镜，随后把异常继续抛给
// Playwright，保证测试仍按失败处理。分镜采集失败只记警告、不改变业务判定。
import { copyFile, mkdir, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { test as baseTest, type Page, type TestInfo } from "@playwright/test";
import {
  FrameRecorder,
  type FrameRecord,
  type StepCard,
  type VideoRecord,
} from "./business-frames";
import {
  renderReportHtml,
  type ReportFrameView,
  type ReportStepView,
  type ReportVideoView,
  type Verdict,
} from "./business-report-html";
import {
  tutorialCopyFor,
  type TutorialCopy,
} from "./business-tutorial-copy";
import {
  renderTutorialHtml,
  type TutorialStepView,
} from "./business-tutorial-html";
import {
  isTutorialMode,
  pauseAfterStep,
  showChapterCard,
} from "./tutorial-visuals";

export interface ReportMeta {
  title: string;
  scenario: string;
  environment: string;
  startedAt: Date;
  appVersion: string;
}

export interface FinalArtifact {
  /** 本地临时文件路径（下载得到的 PDF）。 */
  path: string;
  /** 报告目录内的文件名（中文，便于用户识别）。 */
  name: string;
  /** 对 PDF 的核对结果，逐条展示。 */
  details: string[];
}

export interface StepSpec {
  action: string;
  expected: string;
  /** 纯环境检查 / 控制台检查步骤：允许没有分镜，以状态卡代替。 */
  allowEmptyFrames?: boolean;
  /** 核对最终合同 PDF 的步骤：额外自检三页渲染 + 签名/公章放大图。 */
  requirePdfPages?: boolean;
}

interface StepRecord {
  action: string;
  expected: string;
  actual: string;
  verdict: Verdict;
  frames: FrameRecord[];
  cards: StepCard[];
  videos: VideoRecord[];
  /** 相对报告开始的时间（秒）；视频起点约等于报告开始。 */
  startedAtSeconds: number;
  allowEmptyFrames: boolean;
  requirePdfPages: boolean;
  /** 使用教程文案；没有文案的步骤（控制台检查等）不进教程。 */
  tutorial?: TutorialCopy;
}

/** 报告写出结果：验收报告总是写；使用教程在有教程文案时写。 */
export interface ReportOutput {
  reportPath: string;
  tutorialPath?: string;
}

function errorSummary(error: unknown): string {
  if (error instanceof Error) {
    return error.message.split("\n")[0] ?? error.message;
  }
  return String(error);
}

export class AcceptanceReport {
  private readonly steps: StepRecord[] = [];
  private readonly clockStart = Date.now();

  constructor(
    private readonly meta: ReportMeta,
    /**
     * 报告输出目录：手机侧录像先 saveAs 到这里、读入 Buffer 内嵌后即删除。
     * 缺省时手机侧录像记为「缺失」，报告标注原因，不影响业务判定。
     */
    private readonly outputDir = "",
  ) {}

  /** 应用版本在界面试跑后才读到（/api/v1/health），报告生成前补写。 */
  setAppVersion(version: string): void {
    this.meta.appVersion = version;
  }

  /**
   * 执行一个业务步骤：通过 FrameRecorder 采集分镜、记录判定；失败时记录
   * 实际错误并继续抛出。教程录制模式下，步骤开头显示 1 秒章节标题卡、
   * 结束停顿约 1.2 秒（均不改变业务断言）。
   */
  async runStep(
    page: Page,
    testInfo: TestInfo,
    spec: StepSpec,
    action: (frames: FrameRecorder) => Promise<string>,
  ): Promise<void> {
    const recorder = new FrameRecorder(page, this.outputDir);
    const tutorial = tutorialCopyFor(spec.action);
    const startedAtSeconds = (Date.now() - this.clockStart) / 1000;
    if (tutorial && isTutorialMode()) {
      const tutorialNumber =
        this.steps.filter((step) => step.tutorial && !step.tutorial.seq).length +
        1;
      await showChapterCard(
        page,
        tutorial.seq ?? String(tutorialNumber),
        tutorial.title,
      );
    }
    let actual = "";
    let failure: unknown = null;
    try {
      await baseTest.step(spec.action, async () => {
        actual = await action(recorder);
      });
    } catch (error) {
      failure = error;
      actual = `未达到预期：${errorSummary(error)}`;
    }
    if (tutorial && isTutorialMode()) {
      await pauseAfterStep(page);
    }
    this.steps.push({
      action: spec.action,
      expected: spec.expected,
      actual: actual || "操作完成",
      verdict: failure ? "不符合预期" : "符合预期",
      frames: recorder.frames,
      cards: recorder.cards,
      videos: recorder.videos,
      startedAtSeconds,
      allowEmptyFrames: spec.allowEmptyFrames === true,
      requirePdfPages: spec.requirePdfPages === true,
      tutorial,
    });
    const shots = recorder.frames.filter((frame) => frame.png !== null);
    for (let index = 0; index < shots.length; index += 1) {
      await testInfo.attach(
        `${String(this.steps.length).padStart(2, "0")}-${spec.action}-分镜${index + 1}`,
        { body: shots[index]!.png!, contentType: "image/png" },
      );
    }
    if (failure) {
      throw failure;
    }
  }

  get failedCount(): number {
    return this.steps.filter((step) => step.verdict === "不符合预期").length;
  }

  get passedCount(): number {
    return this.steps.filter((step) => step.verdict === "符合预期").length;
  }

  /** 报告自检：只影响报告呈现，不改变业务判定。 */
  qualityIssues(): string[] {
    const issues: string[] = [];
    this.steps.forEach((step, index) => {
      const usable = step.frames.filter((frame) => frame.png !== null).length;
      if (!step.allowEmptyFrames && usable < 2) {
        issues.push(
          `第 ${index + 1} 步「${step.action}」只有 ${usable} 张可用分镜（要求 ≥2 张）。`,
        );
      }
      step.frames.forEach((frame, frameIndex) => {
        if (!frame.caption.trim()) {
          issues.push(`第 ${index + 1} 步第 ${frameIndex + 1} 张分镜缺少图注。`);
        }
        if (!frame.png) {
          issues.push(
            `第 ${index + 1} 步分镜「${frame.caption}」截图采集失败：${frame.warning ?? "未知原因"}`,
          );
        }
      });
      if (step.requirePdfPages) {
        const pages = step.frames.filter((frame) => frame.kind === "pdf-page").length;
        const zooms = step.frames.filter((frame) => frame.kind === "pdf-zoom").length;
        if (pages < 3) {
          issues.push(`核对最终合同 PDF 只有 ${pages} 张页面渲染图（要求 3 页）。`);
        }
        if (zooms < 1) {
          issues.push("核对最终合同 PDF 缺少签名/公章放大图。");
        }
      }
    });
    return issues;
  }

  /** 生成报告并复制最终 PDF；返回报告与（可选的）使用教程绝对路径。 */
  async write(
    outputDir: string,
    artifact?: FinalArtifact,
  ): Promise<ReportOutput> {
    await mkdir(outputDir, { recursive: true });
    let artifactLink = "";
    if (artifact) {
      await copyFile(artifact.path, join(outputDir, artifact.name));
      artifactLink = artifact.name;
    }
    const html = renderReportHtml({
      meta: this.meta,
      steps: this.stepViews(),
      artifact,
      artifactLink,
      qualityIssues: this.qualityIssues(),
      passedCount: this.passedCount,
      failedCount: this.failedCount,
    });
    const reportPath = join(outputDir, "业务验收报告.html");
    await writeFile(reportPath, html, "utf8");
    const tutorialPath = await this.writeTutorial(outputDir, artifact, artifactLink);
    return { reportPath, tutorialPath };
  }

  /**
   * 使用教程：与验收报告同源（同一批分镜与步骤时间），只渲染有教程文案的
   * 步骤，并去掉测试元信息（控制台检查、报告自检、转换器 API 状态卡）。
   */
  private async writeTutorial(
    outputDir: string,
    artifact: FinalArtifact | undefined,
    artifactLink: string,
  ): Promise<string | undefined> {
    const steps = this.tutorialStepViews();
    if (steps.length === 0) {
      return undefined;
    }
    const numbered = steps.filter((step) => step.seq !== "准备").length;
    const html = renderTutorialHtml({
      title: "从一份 Word 合同，到签字盖章的最终 PDF",
      lead:
        `跟着 ${numbered} 步走一遍完整流程。每一步都配「你要做什么 / ` +
        "你会看到什么 / 注意什么」，截图与录像来自真实产品的一次完整演示。",
      version: this.meta.appVersion,
      slowRecording: isTutorialMode(),
      steps,
      artifactLink,
      artifactName: artifact?.name ?? "",
    });
    const tutorialPath = join(outputDir, "腾手使用教程.html");
    await writeFile(tutorialPath, html, "utf8");
    return tutorialPath;
  }

  private videoViews(videos: VideoRecord[]): ReportVideoView[] {
    return videos.map((video) => ({
      label: video.label,
      webm: video.webm,
      warning: video.warning,
    }));
  }

  private tutorialStepViews(): TutorialStepView[] {
    let number = 0;
    return this.steps
      .filter((step) => step.tutorial !== undefined)
      .map((step) => {
        const copy = step.tutorial!;
        return {
          seq: copy.seq ?? String(++number),
          title: copy.title,
          do: copy.do,
          see: copy.see,
          note: copy.note,
          frames: step.frames.map(
            (frame): ReportFrameView => ({
              caption: frame.caption,
              png: frame.png,
              kind: frame.kind,
              warning: frame.warning,
            }),
          ),
          videos: this.videoViews(step.videos),
          startedAtSeconds: step.startedAtSeconds,
        };
      });
  }

  private stepViews(): ReportStepView[] {
    return this.steps.map((step) => ({
      action: step.action,
      expected: step.expected,
      actual: step.actual,
      verdict: step.verdict,
      frames: step.frames.map(
        (frame): ReportFrameView => ({
          caption: frame.caption,
          png: frame.png,
          kind: frame.kind,
          warning: frame.warning,
        }),
      ),
      videos: this.videoViews(step.videos),
      cards: step.cards,
      startedAtSeconds: step.startedAtSeconds,
    }));
  }
}
