// 场景 C：真实业务场景验收（E2E-2a）。
//
// 业务主线：一份农产品购销合同从 Word 模板 → 界面标定字段/签名/公章 →
// 建合同 → 手机扫码手写签名 → 导入公章并关联 → 预览 → 生成 → 下载。
// 每一步记录「业务动作 / 预期结果 / 实际结果 / 判定 + 截图」，测试结束
// （含失败与跳过）生成自包含中文报告 `业务验收报告.html` 与最终合同 PDF。
//
// 本场景在 Linux / Windows / macOS 三端 CI 都真实运行（e2e job 安装
// LibreOffice，Windows 亦可使用已安装的 Microsoft Word）。本机没有可用
// 转换器且未设 E2E_REQUIRE_DOCX=1 时以中文原因跳过；CI 三端均设
// E2E_REQUIRE_DOCX=1，转换器缺失必须失败而不是跳过。
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

const REQUIRE_DOCX = process.env.E2E_REQUIRE_DOCX === "1";
const OUTPUT_DIR = join(repoRoot(), "e2e", "test-results", "business-acceptance");
const PDF_NAME = "农产品购销合同-已签字盖章.pdf";

/** 报告里用人类可读的平台名，而不是 process.platform 的 node 取值。 */
function platformLabel(): string {
  switch (process.platform) {
    case "win32":
      return "Windows";
    case "darwin":
      return "macOS";
    case "linux":
      return "Linux";
    default:
      return process.platform;
  }
}

/** 浏览器项目与桌面壳引擎的对应关系（已知近似，报告如实标注）。 */
function browserLabel(projectName: string): string {
  if (projectName === "webkit") {
    return "WebKit（近似桌面壳 WKWebView）";
  }
  if (platformLabel() === "Windows") {
    return "Chromium（近似桌面壳 WebView2）";
  }
  return "Chromium";
}

/** 模板详情的 converter 字段 → 报告里的中文名称。 */
function converterNameLabel(name: string): string {
  switch (name) {
    case "word":
      return "Microsoft Word";
    case "libreoffice":
      return "LibreOffice";
    case "pdf":
      return "PDF 直接导入";
    default:
      return name || "未知";
  }
}

interface ConverterItem {
  name?: string;
  label?: string;
  available?: boolean;
  detail?: string;
}

/**
 * 如实描述转换器 API 的返回：Windows 上 Word 的 available 只表示
 * PowerShell 可启动（产品会先尝试 COM 导出，未安装 Word 时回退
 * LibreOffice），不能据此声称「Word 可用」。
 */
function describeConverters(items: ConverterItem[]): string {
  return items
    .map((item) => {
      const label = item.label ?? item.name ?? "未知转换器";
      if (!item.available) {
        return `${label}：不可用${item.detail ? `（${item.detail}）` : ""}`;
      }
      if (item.name === "word") {
        return (
          `${label}：平台可尝试（检测到 ${item.detail ?? "PowerShell"}；` +
          "该检测只证明 Windows 会先尝试 COM 导出，不代表已安装 Word；" +
          "未安装时自动回退 LibreOffice）"
        );
      }
      return `${label}：可用${item.detail ? `（${item.detail}）` : ""}`;
    })
    .join("；");
}

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
      environment:
        `${platformLabel()} · ${browserLabel(testInfo.project.name)} · ` +
        "发布形态 tengshou-cli serve",
      startedAt: new Date(),
      appVersion: "读取中…",
    });
    let pdfPath = "";
    const pdfDetails: string[] = [];

    // try 包住整个场景（含转换器预检与 test.skip）：无论成功、失败还是跳过，
    // finally 都会写出报告并 attach，失败原因出现在「实际结果」里。
    try {
      const health = await page.request.get(`${app.baseURL}/api/v1/health`);
      const appVersion =
        ((await health.json()) as { version?: string }).version ?? "未知";
      report.setAppVersion(appVersion);

      await report.runStep(
        page,
        testInfo,
        {
          action: "确认 Word → PDF 转换器可用",
          expected:
            "本机已安装 Microsoft Word 或 LibreOffice，能把 .docx 合同模板转成 PDF。",
        },
        async () => {
          const converters = await page.request.get(
            `${app.baseURL}/app/api/v1/converters`,
          );
          const converterData = (await converters.json()) as {
            items?: ConverterItem[];
          };
          const items = converterData.items ?? [];
          const usable = items.find((item) => item.available);
          if (!usable) {
            const reason =
              "本机未安装可用的 Word→PDF 转换器（Microsoft Word 或 LibreOffice），" +
              "无法把 Word 合同模板转成 PDF；真实业务场景验收需要转换器。" +
              "请安装 LibreOffice（macOS / Windows / Linux 均可）或 Windows 上的 " +
              "Microsoft Word 后重试。";
            if (REQUIRE_DOCX) {
              throw new Error(
                `${reason}（E2E_REQUIRE_DOCX=1：CI 必须真实运行本场景）`,
              );
            }
            test.skip(true, reason);
          }
          return (
            `转换器 API 状态：${describeConverters(items)}。` +
            "实际使用的转换器在模板导入后以模板详情的 converter 字段为准。"
          );
        },
      );

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
          return `界面已打开（${app.baseURL}/app/），顶栏标签与实时状态正常；转换器预检见上一步。`;
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
          expect(imported.converter).not.toBe("");
          return (
            `Word 文档已转为 PDF 并打开标定编辑器，共 ${templatePageCount} 页；` +
            `模板「${TEMPLATE_NAME}」v1 已入库；实际使用的转换器：` +
            `${converterNameLabel(imported.converter)}` +
            `（模板详情 converter=${imported.converter}）。`
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

      // extras 作为 runStep 的额外截图参数传入（报告会统一 attach 一次），
      // 因此声明在调用之前，回调内只负责收集签名页与桌面端截图。
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
