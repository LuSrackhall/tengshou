// 业务验收报告 HTML 的常驻断言（纯数据，不启动浏览器）。
//
// renderVideos 把手机侧录像以 WebM base64 内联进对应步骤；采集失败时渲染
// 「手机侧录像缺失」说明而不是坏播放器。教程 HTML 已有同类断言
// （tutorial-html.spec.ts），这里用最小 ReportModel 固化验收报告渲染器的
// 两条契约；真实产物的内嵌断言在 business-acceptance.spec.ts 运行时执行。
import { expect, test } from "@playwright/test";
import {
  renderReportHtml,
  type ReportModel,
} from "./support/business-report-html";

/** 合成 4 字节 WebM 头，只验证内联与渲染契约，不当作可播放视频。 */
const WEBM_BYTES = Buffer.from([0x1a, 0x45, 0xdf, 0xa3]);

function minimalModel(): ReportModel {
  return {
    meta: {
      title: "从一份 Word 合同，到签字盖章的最终 PDF",
      scenario: "最小渲染模型",
      environment: "test",
      startedAt: new Date("2026-01-01T00:00:00Z"),
      appVersion: "v0.0.0-test",
    },
    steps: [
      {
        action: "打印自助录入海报，租户手机扫码建档",
        expected: "手机扫码后完成自助建档，桌面收到档案。",
        actual: "操作完成",
        verdict: "符合预期",
        frames: [
          {
            caption: "① 红框处是长期有效的二维码",
            png: WEBM_BYTES,
            kind: "screenshot",
          },
        ],
        videos: [
          {
            label: "手机侧实录：租户扫码后选择类型、填写档案并提交",
            webm: WEBM_BYTES,
          },
        ],
        cards: [],
        startedAtSeconds: 12.3,
      },
    ],
    artifactLink: "",
    qualityIssues: [],
    passedCount: 1,
    failedCount: 0,
  };
}

test("验收报告 HTML：手机侧录像以 WebM base64 内嵌并带中文标签", () => {
  const html = renderReportHtml(minimalModel());

  expect(html.match(/class="step-videos"/g)).toHaveLength(1);
  expect(html).toContain(
    '<video controls preload="metadata" src="data:video/webm;base64,',
  );
  expect(html).toContain(
    `src="data:video/webm;base64,${WEBM_BYTES.toString("base64")}"></video>`,
  );
  expect(html).toContain("手机侧实录：租户扫码后选择类型、填写档案并提交");
  expect(html).toContain("手机侧实录（扫码后手机上的完整录入过程）");
  // 播放器放在步骤头部、分镜之前：扫码后的手机过程要第一眼看到（te-fp48w）。
  const videosAt = html.indexOf('<div class="step-videos">');
  const framesAt = html.indexOf('<div class="frames">');
  expect(videosAt).toBeGreaterThan(-1);
  expect(framesAt).toBeGreaterThan(videosAt);
  expect(html).not.toContain("手机侧录像缺失");
});

test("验收报告 HTML：手机侧录像缺失时标注原因，不渲染坏播放器", () => {
  const model = minimalModel();
  model.steps[0]!.videos = [
    {
      label: "手机侧实录：手机上打开签名页、手写三笔并提交",
      webm: null,
      warning: "该页面未启用录像（video 为 null）",
    },
  ];
  const html = renderReportHtml(model);

  expect(html).toContain("手机侧录像缺失：该页面未启用录像（video 为 null）");
  expect(html).toContain("手机侧实录：手机上打开签名页、手写三笔并提交");
  expect(html).not.toContain("data:video/webm");
});
