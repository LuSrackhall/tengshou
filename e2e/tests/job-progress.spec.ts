// 长操作真实进度（te-rkzf6，ACC-1/2/3/5）：
//   1) 导入 Word 模板时弹窗内可见真实阶段切换（至少「转换为 PDF」）与已用
//      秒数；完成后弹窗关闭并自动打开标定编辑器。
//   2) 批量生成 2 份合同：进度面板由服务端真实 current/total 驱动（i/N），
//      完成后结果表逐份成功；同时用接口旁证 job 快照的 i/N 递增。
//   3) Excel 导入预览/提交走 job 端点：空模板能预览并提交，界面结果正常。
//
// 导入用仓库内的 .docx 夹具走产品转换（CI 三端都安装转换器）；批量用接口
// 准备 PDF 模板与合同，界面操作与断言仍全部走发布形态。
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import type { Page } from "@playwright/test";
import { repoRoot } from "./support/app";
import { expect, test } from "./support/app-fixture";
import { appUrl } from "./support/business-api";
import { shot, trackConsoleErrors } from "./support/ui";

const API_HEADERS = { "X-Tengshou-App": "1" };
const DOCX_FIXTURE = join(
  repoRoot(),
  "e2e",
  "fixtures",
  "contracts",
  "房屋租赁合同.docx",
);
const PDF_FIXTURE = join(repoRoot(), "e2e", "fixtures", "template.pdf");

interface JobSample {
  current: number;
  total: number;
  stage: string;
  message: string;
}

interface JobSnapshot {
  status: string;
  stage: string;
  message: string;
  current: number;
  total: number;
  error?: { code: string; message: string };
}

declare global {
  interface Window {
    __jobSamples?: JobSample[];
    __jobSampleTimer?: number;
  }
}

/** 在页面内每 40ms 采样进度面板，避免 400ms 轮询节奏漏掉中间 i/N。 */
async function startJobSampling(page: Page): Promise<void> {
  await page.evaluate(() => {
    window.__jobSamples = [];
    if (window.__jobSampleTimer) {
      window.clearInterval(window.__jobSampleTimer);
    }
    window.__jobSampleTimer = window.setInterval(() => {
      const panel = document.querySelector<HTMLElement>(".job-progress");
      if (!panel) {
        return;
      }
      window.__jobSamples?.push({
        current: Number(panel.getAttribute("data-job-current") ?? "0"),
        total: Number(panel.getAttribute("data-job-total") ?? "0"),
        stage: panel.getAttribute("data-job-stage") ?? "",
        message:
          panel.querySelector(".job-progress-message")?.textContent?.trim() ?? "",
      });
    }, 40);
  });
}

async function stopJobSampling(page: Page): Promise<JobSample[]> {
  return page.evaluate(() => {
    if (window.__jobSampleTimer) {
      window.clearInterval(window.__jobSampleTimer);
      window.__jobSampleTimer = undefined;
    }
    return window.__jobSamples ?? [];
  });
}

test.describe("长操作真实进度（job 化）", () => {
  test("模板导入显示转换阶段与已用秒数，完成后自动进入标定", async ({
    app,
    page,
  }, testInfo) => {
    const consoleErrors = trackConsoleErrors(page);
    const templateName = `E2E 进度模板 ${Date.now().toString(36)}`;

    await page.goto(`${app.baseURL}/app/`, { waitUntil: "domcontentloaded" });
    await expect(page.locator("#tabs")).toBeVisible();
    await page.locator('#tabs button[data-tab="templates"]').click();
    await expect(page.locator("#view-templates")).toHaveClass(/active/);
    await startJobSampling(page);

    await page.locator('#view-templates input[type="file"]').setInputFiles(DOCX_FIXTURE);
    const dialog = page
      .getByRole("dialog")
      .filter({ has: page.getByRole("heading", { name: /导入模板/ }) });
    await expect(dialog).toBeVisible();
    await dialog.locator("#tplImportName").fill(templateName);
    await dialog.getByRole("button", { name: "导入", exact: true }).click();

    // ACC-1：导入阶段可见（至少「转换为 PDF」），并显示已用秒数。
    const panel = dialog.locator(".job-progress");
    await expect(panel).toBeVisible({ timeout: 15_000 });
    await expect(panel).toHaveAttribute("data-job-status", "running");
    await expect
      .poll(async () => panel.getAttribute("data-job-stage"), { timeout: 60_000 })
      .toBe("converting");
    await expect(panel.locator(".job-progress-message")).toContainText(
      "正在转换为 PDF",
    );
    await expect(panel.locator(".job-progress-elapsed")).toContainText(
      /已用 \d+ 秒/,
    );
    // 等待期间允许关闭：按钮文案明确「转到后台」。
    await expect(
      dialog.getByRole("button", { name: "转到后台", exact: true }),
    ).toBeVisible();
    await shot(page, testInfo, "导入阶段进度可见");

    // 完成后照旧自动打开标定编辑器。
    await expect(dialog).toBeHidden({ timeout: 240_000 });
    await expect(page.locator("#tplCloseBtn")).toBeVisible({ timeout: 120_000 });
    const samples = await stopJobSampling(page);
    expect(samples.some((sample) => sample.stage === "converting")).toBe(true);
    await page.locator("#tplCloseBtn").click();
    expect(consoleErrors).toEqual([]);
  });

  test("批量生成 2 份合同显示真实 i/N 并逐份成功", async ({ app, page }, testInfo) => {
    const consoleErrors = trackConsoleErrors(page);
    const stamp = Date.now().toString(36);

    await page.goto(`${app.baseURL}/app/`, { waitUntil: "domcontentloaded" });
    await expect(page.locator("#tabs")).toBeVisible();

    // 准备：PDF 模板（无标定位置也能生成）与两份合同。
    const pdfBase64 = (await readFile(PDF_FIXTURE)).toString("base64");
    const templateResponse = await page.request.post(
      appUrl(page, "/app/api/v1/templates/import"),
      {
        headers: API_HEADERS,
        data: {
          name: `E2E 批量进度模板 ${stamp}`,
          file_name: "batch-progress.pdf",
          file_base64: pdfBase64,
        },
      },
    );
    expect(templateResponse.ok(), await templateResponse.text()).toBeTruthy();
    const template = (await templateResponse.json()) as { item?: { id?: string } };
    const templateId = template.item?.id ?? "";
    expect(templateId).not.toBe("");

    const partyResponse = await page.request.post(
      appUrl(page, "/app/api/v1/parties"),
      {
        headers: API_HEADERS,
        data: {
          kind: "个人",
          name: `E2E 批量租户 ${stamp}`,
          id_number: `330102199001${stamp.slice(-4).padStart(4, "0")}`,
        },
      },
    );
    expect(partyResponse.ok(), await partyResponse.text()).toBeTruthy();
    const party = (await partyResponse.json()) as { item?: { id?: string } };
    const partyId = party.item?.id ?? "";
    expect(partyId).not.toBe("");

    const contractIds: string[] = [];
    for (const index of [1, 2]) {
      const contractResponse = await page.request.post(
        appUrl(page, "/app/api/v1/contracts"),
        {
          headers: API_HEADERS,
          data: {
            title: `E2E 批量进度合同 ${index} ${stamp}`,
            party_b_id: partyId,
            template_id: templateId,
            status: "草稿",
          },
        },
      );
      expect(contractResponse.ok(), await contractResponse.text()).toBeTruthy();
      const contract = (await contractResponse.json()) as {
        item?: { id?: string };
      };
      const contractId = contract.item?.id ?? "";
      expect(contractId).not.toBe("");
      contractIds.push(contractId);
    }

    // 接口旁证（ACC-2 的数据面）：批量生成 job 的 current/total 是真实 i/N，
    // 结束时 current=2、total=2，过程中出现「第 x/2 份」文案。
    const previews: Array<{ contract_id: string; preview_sha256: string }> = [];
    for (const contractId of contractIds) {
      const preflight = await page.request.post(
        appUrl(page, `/app/api/v1/templates/${templateId}/preflight`),
        { headers: API_HEADERS, data: { contract_id: contractId } },
      );
      expect(preflight.ok(), await preflight.text()).toBeTruthy();
      const result = (await preflight.json()) as { preview_sha256?: string };
      previews.push({
        contract_id: contractId,
        preview_sha256: result.preview_sha256 ?? "",
      });
    }
    const startJob = await page.request.post(
      appUrl(page, `/app/api/v1/jobs/templates/${templateId}/batch-generate`),
      { headers: API_HEADERS, data: { items: previews } },
    );
    expect(startJob.status()).toBe(202);
    const started = (await startJob.json()) as { job_id?: string };
    const jobId = started.job_id ?? "";
    expect(jobId).not.toBe("");
    const snapshots: JobSnapshot[] = [];
    const deadline = Date.now() + 120_000;
    for (;;) {
      const response = await page.request.get(
        appUrl(page, `/app/api/v1/jobs/${jobId}`),
      );
      expect(response.ok(), await response.text()).toBeTruthy();
      const snapshot = (await response.json()) as JobSnapshot;
      snapshots.push(snapshot);
      if (snapshot.status !== "running") {
        break;
      }
      if (Date.now() > deadline) {
        throw new Error("批量生成 job 在 120s 内未结束");
      }
      await new Promise((resolve) => setTimeout(resolve, 25));
    }
    const finalSnapshot = snapshots[snapshots.length - 1];
    expect(finalSnapshot.status).toBe("done");
    expect(finalSnapshot.total).toBe(2);
    expect(finalSnapshot.current).toBe(2);
    const progressSnapshots = snapshots.filter(
      (snapshot) => snapshot.total === 2,
    );
    expect(progressSnapshots.length).toBeGreaterThan(0);
    const apiCurrents = progressSnapshots.map((snapshot) => snapshot.current);
    for (let index = 1; index < apiCurrents.length; index += 1) {
      expect(apiCurrents[index]).toBeGreaterThanOrEqual(
        apiCurrents[index - 1] ?? 0,
      );
    }
    expect(
      snapshots.some((snapshot) => /第 \d+\/2 份/.test(snapshot.message)),
    ).toBe(true);

    // 界面路径：重新加载让台账取到新建的合同，勾选两份后批量生成。
    await page.goto(`${app.baseURL}/app/`, { waitUntil: "domcontentloaded" });
    await expect(page.locator("#tabs")).toBeVisible();
    await page.locator('#tabs button[data-tab="contracts"]').click();
    for (const contractId of contractIds) {
      await page.locator(`input[data-select="${contractId}"]`).check();
    }
    await page.locator("#batchGenerateBtn").click();
    const batch = page
      .getByRole("dialog")
      .filter({ has: page.getByRole("heading", { name: /批量生成/ }) });
    await expect(batch).toBeVisible();
    await startJobSampling(page);

    // 预检走 job：等待期间进度面板渲染（采样验证），完成后摘要给出真实结果。
    // 2 份的小批量可能在一次 400ms 轮询内完成，中间 i/N 未必落在面板上；
    // 服务端 current/total 已由上面的接口快照断言为真实递增。
    await batch.locator("#batchPreflight").click();
    await expect(batch.locator("#batchSummary")).toContainText("可生成 2 份", {
      timeout: 120_000,
    });

    page.once("dialog", (confirm) => confirm.accept());
    await batch
      .getByRole("button", { name: "开始批量生成", exact: true })
      .click();
    await expect(batch.locator("#batchResults tbody tr")).toHaveCount(2, {
      timeout: 180_000,
    });
    await expect(batch.locator("#batchResults tbody")).toContainText("成功");
    await shot(page, testInfo, "批量生成完成结果");

    const samples = await stopJobSampling(page);
    // 进度面板确实渲染过（job 化后的界面路径），且任何带真实 total 的快照
    // 都落在 0..total 区间并给出「第 x/y 份」文案。
    expect(samples.length).toBeGreaterThan(0);
    for (const sample of samples) {
      if (sample.total <= 0) {
        continue;
      }
      expect(sample.current).toBeGreaterThanOrEqual(0);
      expect(sample.current).toBeLessThanOrEqual(sample.total);
      expect(sample.message).toMatch(/第 \d+\/\d+ 份/);
    }
    expect(consoleErrors).toEqual([]);
  });

  test("Excel 导入预览与提交走后台任务，界面结果正常", async ({ app, page }) => {
    const consoleErrors = trackConsoleErrors(page);
    const dir = await mkdtemp(join(tmpdir(), "tengshou-job-progress-"));
    const workbookPath = join(dir, "entry-template.xlsx");
    try {
      const templateResponse = await page.request.get(
        `${app.baseURL}/app/api/v1/excel/template`,
        { headers: API_HEADERS },
      );
      expect(templateResponse.ok()).toBeTruthy();
      await writeFile(workbookPath, await templateResponse.body());

      await page.goto(`${app.baseURL}/app/`, { waitUntil: "domcontentloaded" });
      await expect(page.locator("#tabs")).toBeVisible();
      await page.locator('#tabs button[data-tab="contracts"]').click();
      await page.locator("#importFile").setInputFiles(workbookPath);

      const dialog = page
        .getByRole("dialog")
        .filter({ has: page.getByRole("heading", { name: /导入 Excel/ }) });
      await expect(dialog).toBeVisible();
      // 预览（job）完成后出现工作表说明与选项区。
      await expect(dialog.getByText(/导入前先预览/)).toBeVisible({
        timeout: 120_000,
      });
      await dialog
        .getByRole("button", { name: "确认导入", exact: true })
        .click();
      // 提交（job）完成：结果区出现计数徽标。
      const resultArea = dialog.locator("#importResult .import-summary");
      await expect(resultArea).toBeVisible({ timeout: 120_000 });
      await expect(resultArea).toContainText("新增");
      await dialog.getByRole("button", { name: "取消", exact: true }).click();
      await expect(dialog).toBeHidden();
      expect(consoleErrors).toEqual([]);
    } finally {
      await rm(dir, { recursive: true, force: true });
    }
  });
});
