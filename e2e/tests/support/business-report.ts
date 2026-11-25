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
} from "./business-frames";
import {
  renderReportHtml,
  type ReportFrameView,
  type ReportStepView,
  type Verdict,
} from "./business-report-html";

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
  /** 相对报告开始的时间（秒）；视频起点约等于报告开始。 */
  startedAtSeconds: number;
  allowEmptyFrames: boolean;
  requirePdfPages: boolean;
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

  constructor(private readonly meta: ReportMeta) {}

  /** 应用版本在界面试跑后才读到（/api/v1/health），报告生成前补写。 */
  setAppVersion(version: string): void {
    this.meta.appVersion = version;
  }

  /**
   * 执行一个业务步骤：通过 FrameRecorder 采集分镜、记录判定；失败时记录
   * 实际错误并继续抛出。
   */
  async runStep(
    page: Page,
    testInfo: TestInfo,
    spec: StepSpec,
    action: (frames: FrameRecorder) => Promise<string>,
  ): Promise<void> {
    const recorder = new FrameRecorder(page);
    const startedAtSeconds = (Date.now() - this.clockStart) / 1000;
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
    this.steps.push({
      action: spec.action,
      expected: spec.expected,
      actual: actual || "操作完成",
      verdict: failure ? "不符合预期" : "符合预期",
      frames: recorder.frames,
      cards: recorder.cards,
      startedAtSeconds,
      allowEmptyFrames: spec.allowEmptyFrames === true,
      requirePdfPages: spec.requirePdfPages === true,
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

  /** 生成报告并复制最终 PDF；返回报告绝对路径。 */
  async write(outputDir: string, artifact?: FinalArtifact): Promise<string> {
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
    return reportPath;
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
      cards: step.cards,
      startedAtSeconds: step.startedAtSeconds,
    }));
  }
}
