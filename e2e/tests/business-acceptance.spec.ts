// 场景 C：真实业务场景验收（E2E-2a）。
//
// 业务主线：一份农产品购销合同从 Word 模板 → 界面标定字段/签名/公章 →
// 建合同 → 手机扫码手写签名 → 导入公章并关联 → 预览 → 生成 → 下载。
// 每一步记录「业务动作 / 预期结果 / 实际结果 / 判定 + 截图」，测试结束
// （含失败）生成自包含中文报告 `业务验收报告.html` 与最终合同 PDF。
//
// 本场景默认只在 Linux CI 运行（e2e job 安装 LibreOffice + 中文字体）；
// 本地调试可设 E2E_BUSINESS_NON_LINUX=1，并自行安装 LibreOffice。CI 设
// E2E_REQUIRE_DOCX=1 时，转换器缺失必须失败而不是跳过。
import { readFile } from "node:fs/promises";
import { join } from "node:path";
import { expect, test } from "./support/business-fixture";
import { repoRoot } from "./support/app";
import { AcceptanceReport } from "./support/business-report";
import {
  CONTRACT_TITLE,
  PARTY_A_NAME,
  PARTY_A_VALUES,
  PARTY_B_NAME,
  PARTY_B_VALUES,
  PLACEMENTS,
  TEMPLATE_NAME,
} from "./support/business-scenario";
import {
  calibrateTemplate,
  createContract,
  createFields,
  createParty,
  generateContract,
  importAndAttachSeal,
  importTemplate,
  previewContract,
  saveCalibration,
  signByPhone,
  verifyFinalPdf,
} from "./support/business-ui";
import { trackConsoleErrors } from "./support/ui";

const LINUX = process.platform === "linux";
const FORCE_NON_LINUX = process.env.E2E_BUSINESS_NON_LINUX === "1";
const REQUIRE_DOCX = process.env.E2E_REQUIRE_DOCX === "1";
const OUTPUT_DIR = join(repoRoot(), "e2e", "test-results", "business-acceptance");
const PDF_NAME = "农产品购销合同-已签字盖章.pdf";

test.skip(
  !LINUX && !FORCE_NON_LINUX,
  "本场景默认只在 Linux Chromium 上运行（CI 的 e2e Linux job 已安装 LibreOffice 与中文字体）；" +
    "本地调试可设置 E2E_BUSINESS_NON_LINUX=1 并自行安装 LibreOffice。",
);
test.describe.configure({ timeout: 600_000 });

test.describe("场景 C：真实业务场景验收（Word 合同 → 签字盖章 → 最终 PDF）", () => {
  test("Word 模板导入 → 标定字段/签名/公章 → 建合同 → 扫码签名 → 公章 → 预览 → 生成 → 下载", async ({
    app,
    page,
  }, testInfo) => {
    const consoleErrors = trackConsoleErrors(page);
    const report = new AcceptanceReport({
      title: "业务验收报告 · 农产品购销合同",
      scenario:
        "真实业务场景：幸福村股份经济合作社向农户王建国采购红富士苹果，从 Word 合同模板开始，" +
        "在腾手里完成字段标定、双方档案、合同录入、手机手写签名、公章落位，最终生成并下载可打开的合同 PDF。",
      environment: `${process.platform} · ${testInfo.project.name} · 发布形态 tengshou-cli serve`,
      startedAt: new Date(),
      appVersion: "读取中…",
    });
    let pdfPath = "";
    const pdfDetails: string[] = [];

    const health = await page.request.get(`${app.baseURL}/api/v1/health`);
    const appVersion =
      ((await health.json()) as { version?: string }).version ?? "未知";
    report.setAppVersion(appVersion);

    const converters = await page.request.get(
      `${app.baseURL}/app/api/v1/converters`,
    );
    const converterData = (await converters.json()) as {
      items?: Array<{ label: string; available: boolean }>;
    };
    const usableConverter = (converterData.items ?? []).find(
      (item) => item.available,
    );
    if (!usableConverter) {
      const reason =
        "本机未安装 Microsoft Word / LibreOffice，无法把 Word 合同模板转成 PDF；" +
        "真实业务场景验收需要转换器。安装 LibreOffice 后重试。";
      if (REQUIRE_DOCX) {
        throw new Error(`${reason}（E2E_REQUIRE_DOCX=1：CI 必须真实运行本场景）`);
      }
      test.skip(true, reason);
    }

    const docxPath = join(
      repoRoot(),
      "e2e",
      "fixtures",
      "contracts",
      "农产品购销合同.docx",
    );
    const sealPath = join(
      repoRoot(),
      "e2e",
      "fixtures",
      "contracts",
      "采购方公章.png",
    );
    let templateId = "";
    let templatePageCount = 0;
    let contractId = "";
    let contractNo = "";
    let partyAId = "";

    try {
      await report.runStep(
        page,
        testInfo,
        {
          action: "打开发布形态界面",
          expected: "腾手合同台账正常打开，顶部标签栏与实时状态可见。",
        },
        async () => {
          const response = await page.goto(`${app.baseURL}/app/`, {
            waitUntil: "domcontentloaded",
          });
          expect(response?.status()).toBe(200);
          await expect(page.locator("#tabs")).toBeVisible();
          await expect(page.locator(".live-status")).toBeVisible();
          return `界面已打开（${app.baseURL}/app/），顶栏标签与实时状态正常；可用转换程序：${usableConverter?.label}。`;
        },
      );

      await report.runStep(
        page,
        testInfo,
        {
          action: "导入 Word 合同模板",
          expected:
            "选择仓库内的 .docx 后，产品把它转换成 PDF 并自动打开标定编辑器。",
        },
        async () => {
          const imported = await importTemplate(page, docxPath, TEMPLATE_NAME);
          templateId = imported.templateId;
          templatePageCount = imported.pageCount;
          return (
            `Word 已转换并打开标定编辑器，共 ${templatePageCount} 页；` +
            `模板「${TEMPLATE_NAME}」v1 已入库。`
          );
        },
      );

      await report.runStep(
        page,
        testInfo,
        {
          action: "建立合同字段与相对方联系人字段",
          expected:
            "在「字段」页建立合同专有字段（≥6 个）和相对方联系人字段，保存后立即可用。",
        },
        async () => await createFields(page),
      );

      await report.runStep(
        page,
        testInfo,
        {
          action: "建立甲方（单位）与乙方（个人）档案",
          expected:
            "甲方合作社与乙方农户的姓名、证件号、地址、电话、联系人分别保存为两份档案。",
        },
        async () => {
          partyAId = await createParty(page, PARTY_A_VALUES);
          const partyBId = await createParty(page, PARTY_B_VALUES);
          expect(partyAId).not.toBe("");
          expect(partyBId).not.toBe("");
          return (
            `甲方档案「${PARTY_A_NAME}（单位）」与乙方档案「${PARTY_B_NAME}（个人）」已保存；` +
            "联系人李国强写入甲方档案。"
          );
        },
      );

      await report.runStep(
        page,
        testInfo,
        {
          action: "在 PDF 页面上逐框标定字段、签名与公章位置",
          expected:
            "按 Word 里的「字段名：＿＿」位置，把 24 个字段、乙方签名与甲方公章位置逐个拖到对应框内。",
        },
        async () =>
          await calibrateTemplate(page, templateId, PLACEMENTS),
      );

      await report.runStep(
        page,
        testInfo,
        {
          action: "保存标定为新模板版本",
          expected: "点击「保存为新版本」后生成 v2，位置列表保留全部 26 个位置。",
        },
        async () =>
          `标定已保存为新版本：${await saveCalibration(page, PLACEMENTS.length)}；旧版本 v1 保留。`,
      );

      await report.runStep(
        page,
        testInfo,
        {
          action: "新建合同：选择甲方、相对方并填写标的与金额",
          expected:
            "合同保存为草稿，编号自动分配；甲方=幸福村合作社，相对方=王建国，金额 160,000.00 元，标的与交付信息齐全。",
        },
        async () => {
          const created = await createContract(page, partyAId);
          contractId = created.contractId;
          contractNo = created.contractNo;
          return (
            `合同 ${contractNo}「${CONTRACT_TITLE}」已保存为草稿；` +
            `甲方=${created.detail.party_a?.name}，相对方=${created.detail.party_b?.name}，` +
            "金额 160,000.00 元。"
          );
        },
      );

      const extras: Buffer[] = [];
      await report.runStep(
        page,
        testInfo,
        {
          action: "扫码手写签名：手机页提交，桌面端收到",
          expected:
            "生成一次性二维码后，第二页面用鼠标手写多笔并提交；手机页显示「签名已提交」，桌面弹窗显示「签名已保存」。",
        },
        async () => {
          const signed = await signByPhone(page, app.baseURL, contractId);
          await testInfo.attach("手机手写签名页", {
            body: signed.phoneShot,
            contentType: "image/png",
          });
          await testInfo.attach("桌面端收到签名", {
            body: signed.desktopShot,
            contentType: "image/png",
          });
          extras.push(signed.phoneShot, signed.desktopShot);
          return signed.actual;
        },
        extras,
      );

      await report.runStep(
        page,
        testInfo,
        {
          action: "导入白底公章图片并关联到合同",
          expected:
            "白底红章被产品自动去底并裁剪，卡片出现在「公章」页，且已关联到本合同。",
        },
        async () =>
          await importAndAttachSeal(page, sealPath, partyAId, contractId),
      );

      await report.runStep(
        page,
        testInfo,
        {
          action: "用目标合同预览版式",
          expected: "生成面板默认选中已标定的模板版本，预览 PDF 能正常打开显示。",
        },
        async () => await previewContract(page, contractId),
      );

      await report.runStep(
        page,
        testInfo,
        {
          action: "生成最终合同 PDF 并下载",
          expected:
            "生成成功后，生成历史出现「成功」，下载得到以 %PDF- 开头的合同文件。",
        },
        async () => {
          pdfPath = testInfo.outputPath(PDF_NAME);
          const generated = await generateContract(page, pdfPath);
          return generated.actual;
        },
      );

      await report.runStep(
        page,
        testInfo,
        {
          action: "核对最终合同 PDF",
          expected:
            "页数与模板一致；文本包含合同编号、金额、双方名称、标的等关键值；签名与公章两张图片确实套印。",
        },
        async () => {
          const bytes = await readFile(pdfPath);
          const verified = await verifyFinalPdf(
            page,
            bytes,
            templatePageCount,
            contractNo,
          );
          pdfDetails.push(...verified.details);
          return verified.actual;
        },
      );

      await report.runStep(
        page,
        testInfo,
        {
          action: "检查浏览器控制台",
          expected: "从打开界面到生成合同，全程没有 console error 或未捕获异常。",
        },
        async () => {
          expect(
            consoleErrors,
            `浏览器控制台出现错误：\n${consoleErrors.join("\n")}`,
          ).toEqual([]);
          return "全程未发现浏览器 console error 或未捕获异常。";
        },
      );
    } finally {
      const artifact = pdfPath
        ? { path: pdfPath, name: PDF_NAME, details: pdfDetails }
        : undefined;
      const reportPath = await report.write(OUTPUT_DIR, artifact);
      await testInfo.attach("业务验收报告.html", {
        path: reportPath,
        contentType: "text/html",
      });
      if (pdfPath) {
        await testInfo.attach("最终合同.pdf", {
          path: pdfPath,
          contentType: "application/pdf",
        });
      }
    }
  });
});
