// 用户可读的「业务验收报告」：把 E2E 的业务动作、预期、实际与判定渲染成
// 自包含单文件 HTML（截图 base64 内嵌、离线可打开），并复制最终合同 PDF。
//
// 失败也会写入报告：失败步骤标红、写明实际结果与截图，随后把异常继续抛给
// Playwright，保证测试仍按失败处理。
import { copyFile, mkdir, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { test as baseTest, type Page, type TestInfo } from "@playwright/test";

export type Verdict = "符合预期" | "不符合预期";

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

interface StepRecord {
  action: string;
  expected: string;
  actual: string;
  verdict: Verdict;
  screenshots: Buffer[];
}

export interface StepSpec {
  action: string;
  expected: string;
}

function escapeHtml(value: string): string {
  return value
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");
}

function errorSummary(error: unknown): string {
  if (error instanceof Error) {
    return error.message.split("\n")[0] ?? error.message;
  }
  return String(error);
}

function formatTime(when: Date): string {
  return when.toLocaleString("zh-CN", { hour12: false });
}

export class AcceptanceReport {
  private readonly steps: StepRecord[] = [];

  constructor(private readonly meta: ReportMeta) {}

  /** 应用版本在界面试跑后才读到（/api/v1/health），报告生成前补写。 */
  setAppVersion(version: string): void {
    this.meta.appVersion = version;
  }

  /** 执行一个业务步骤：结束时截图、记录判定；失败时记录实际错误并继续抛出。 */
  async runStep(
    page: Page,
    testInfo: TestInfo,
    spec: StepSpec,
    action: () => Promise<string>,
    extraScreenshots: Buffer[] = [],
  ): Promise<void> {
    let actual = "";
    let failure: unknown = null;
    try {
      await baseTest.step(spec.action, async () => {
        actual = await action();
      });
    } catch (error) {
      failure = error;
      actual = `未达到预期：${errorSummary(error)}`;
    }
    const screenshot = await this.screenshot(page);
    const shots = screenshot ? [screenshot, ...extraScreenshots] : [...extraScreenshots];
    this.steps.push({
      action: spec.action,
      expected: spec.expected,
      actual: actual || "操作完成",
      verdict: failure ? "不符合预期" : "符合预期",
      screenshots: shots,
    });
    for (let index = 0; index < shots.length; index += 1) {
      await testInfo.attach(
        `${String(this.steps.length).padStart(2, "0")}-${spec.action}-${index + 1}`,
        { body: shots[index]!, contentType: "image/png" },
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

  /** 生成报告并复制最终 PDF；返回报告绝对路径。 */
  async write(
    outputDir: string,
    artifact?: FinalArtifact,
  ): Promise<string> {
    await mkdir(outputDir, { recursive: true });
    let artifactLink = "";
    if (artifact) {
      await copyFile(artifact.path, join(outputDir, artifact.name));
      artifactLink = artifact.name;
    }
    const html = this.render(artifact, artifactLink);
    const reportPath = join(outputDir, "业务验收报告.html");
    await writeFile(reportPath, html, "utf8");
    return reportPath;
  }

  private async screenshot(page: Page): Promise<Buffer | null> {
    try {
      return await page.screenshot({ fullPage: true });
    } catch {
      return null;
    }
  }

  private render(artifact: FinalArtifact | undefined, artifactLink: string): string {
    const total = this.steps.length;
    const verdict =
      this.failedCount === 0
        ? "全部步骤符合预期"
        : `有 ${this.failedCount} 个步骤未达到预期`;
    const summaryClass = this.failedCount === 0 ? "pass" : "fail";
    const steps = this.steps
      .map((step, index) => {
        const shots = step.screenshots
          .map(
            (shot, shotIndex) =>
              `<figure><img alt="${escapeHtml(step.action)} 截图 ${shotIndex + 1}" ` +
              `src="data:image/png;base64,${shot.toString("base64")}"></figure>`,
          )
          .join("");
        const badge = step.verdict === "符合预期" ? "pass" : "fail";
        return (
          `<section class="step ${badge}">` +
          `<header><span class="seq">${index + 1}</span>` +
          `<h2>${escapeHtml(step.action)}</h2>` +
          `<span class="badge ${badge}">${escapeHtml(step.verdict)}</span></header>` +
          `<dl><dt>预期结果</dt><dd>${escapeHtml(step.expected)}</dd>` +
          `<dt>实际结果</dt><dd>${escapeHtml(step.actual)}</dd></dl>` +
          shots +
          `</section>`
        );
      })
      .join("");
    const artifactBlock = artifact
      ? `<section class="artifact"><h2>最终合同</h2>` +
        `<p><a href="${encodeURI(artifactLink)}" download>下载最终合同 PDF：${escapeHtml(artifact.name)}</a></p>` +
        `<ul>${artifact.details.map((item) => `<li>${escapeHtml(item)}</li>`).join("")}</ul></section>`
      : "";
    return `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>${escapeHtml(this.meta.title)}</title>
<style>
  :root { color-scheme: light; }
  * { box-sizing: border-box; }
  body { margin: 0; background: #f2f5f3; color: #1f2328;
    font: 16px/1.7 -apple-system, "PingFang SC", "Microsoft YaHei", "Noto Sans CJK SC", sans-serif; }
  main { max-width: 1040px; margin: 0 auto; padding: 28px 18px 60px; }
  h1 { font-size: 26px; margin: 0 0 6px; }
  .lede { color: #5f6670; margin: 0 0 18px; }
  .panel { background: #fff; border-radius: 14px; padding: 18px 20px; box-shadow: 0 8px 24px rgba(22,34,28,.08); margin-bottom: 18px; }
  .verdict { font-size: 20px; font-weight: 700; margin: 0 0 6px; }
  .verdict.pass { color: #1f6f4a; }
  .verdict.fail { color: #b4342c; }
  .disclaimer { border-left: 4px solid #d9a400; background: #fff8e6; padding: 10px 14px; border-radius: 8px; }
  table.meta { width: 100%; border-collapse: collapse; font-size: 14.5px; }
  table.meta th { text-align: left; color: #5f6670; font-weight: 500; width: 110px; padding: 3px 10px 3px 0; }
  table.meta td { padding: 3px 0; word-break: break-all; }
  .step { background: #fff; border-radius: 14px; padding: 16px 20px 6px; margin-bottom: 16px; box-shadow: 0 6px 18px rgba(22,34,28,.06); border-top: 4px solid #cfd6d1; }
  .step.pass { border-top-color: #1f6f4a; }
  .step.fail { border-top-color: #b4342c; }
  .step header { display: flex; align-items: center; gap: 10px; }
  .step h2 { font-size: 18px; margin: 0; flex: 1; }
  .seq { display: inline-flex; width: 28px; height: 28px; border-radius: 50%; background: #eef1ef; color: #3d4642; align-items: center; justify-content: center; font-weight: 700; font-size: 14px; }
  .badge { font-size: 13px; padding: 3px 10px; border-radius: 999px; white-space: nowrap; }
  .badge.pass { background: #e3f1e9; color: #1f6f4a; }
  .badge.fail { background: #fbe7e5; color: #b4342c; }
  dl { display: grid; grid-template-columns: 90px 1fr; gap: 4px 12px; margin: 12px 0; }
  dt { color: #5f6670; font-size: 14px; }
  dd { margin: 0; }
  figure { margin: 10px 0 14px; }
  figure img { width: 100%; height: auto; border: 1px solid #e2e7e4; border-radius: 8px; display: block; }
  .artifact { background: #fff; border-radius: 14px; padding: 18px 20px; box-shadow: 0 8px 24px rgba(22,34,28,.08); margin-bottom: 18px; }
  .artifact h2 { margin-top: 0; font-size: 18px; }
  .artifact a { color: #1f6f4a; font-weight: 700; }
  footer { color: #7d857f; font-size: 13px; text-align: center; padding-top: 10px; }
  @media (max-width: 640px) { dl { grid-template-columns: 1fr; } dt { margin-top: 6px; } }
</style>
</head>
<body>
<main>
  <h1>${escapeHtml(this.meta.title)}</h1>
  <p class="lede">${escapeHtml(this.meta.scenario)}</p>
  <section class="panel">
    <p class="verdict ${summaryClass}">${escapeHtml(verdict)}（共 ${total} 步：符合预期 ${this.passedCount} 步）</p>
    <div class="disclaimer">本合同中的单位、姓名、证件号、电话、地址、金额与公章编号全部虚构，仅用于演示与测试，不具有任何法律效力。</div>
  </section>
  <section class="panel">
    <table class="meta">
      <tr><th>开始时间</th><td>${escapeHtml(formatTime(this.meta.startedAt))}</td></tr>
      <tr><th>运行环境</th><td>${escapeHtml(this.meta.environment)}</td></tr>
      <tr><th>腾手版本</th><td>${escapeHtml(this.meta.appVersion)}</td></tr>
      <tr><th>测试形态</th><td>发布形态 tengshou-cli serve + go:embed 界面，离线运行</td></tr>
    </table>
  </section>
  ${steps}
  ${artifactBlock}
  <footer>由腾手 E2E 自动生成 · 报告为单文件，截图已内嵌，可离线打开</footer>
</main>
</body>
</html>
`;
  }
}
