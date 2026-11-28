// 使用教程 HTML 的常驻断言（纯数据，不启动浏览器）。
//
// 教程 HTML 是发版资产的一部分（CI `tutorial` job 打成教程包），但此前三段式
// 文案、目录、章节跳转、示例 PDF 链接等内容只在实现期间人工核对，临时检查
// 脚本随后被删除。这里把两类检查固化下来：
// 1) 用最小 TutorialModel 断言 renderTutorialHtml 的渲染契约；
// 2) 核对业务验收 spec 的步骤标题与 TUTORIAL_COPY 键一一对应（测试步骤
//    「检查浏览器控制台」刻意没有教程文案，因此不进教程）。
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { expect, test } from "@playwright/test";
import {
  TUTORIAL_COPY,
  tutorialCopyFor,
} from "./support/business-tutorial-copy";
import {
  TUTORIAL_VIDEO_NAME,
  renderTutorialHtml,
  type TutorialModel,
} from "./support/business-tutorial-html";

/** 只有测试元信息、刻意不写教程文案的步骤。 */
const TEST_ONLY_ACTIONS = new Set(["检查浏览器控制台"]);

function minimalModel(): TutorialModel {
  return {
    title: "从一份 Word 合同，到签字盖章的最终 PDF",
    lead: "跟着 1 步走一遍完整流程。",
    version: "v0.0.0-test",
    slowRecording: true,
    steps: [
      {
        seq: "准备",
        title: "开始前：确认 Word → PDF 转换器可用",
        do: ["确认电脑已安装可用的转换器。"],
        see: ["自检面板显示 Word 转换「可用」。"],
        note: ["macOS 请用 LibreOffice。"],
        frames: [],
        videos: [],
        startedAtSeconds: 0,
      },
      {
        seq: "1",
        title: "导入 Word 合同模板",
        do: ["点「导入模板」并选择 .docx。"],
        see: ["模板列表出现 v1 版本。"],
        note: ["产品不会修改你的原文件。"],
        frames: [
          {
            caption: "① 模板列表出现 v1",
            png: Buffer.from([0x89, 0x50, 0x4e, 0x47]),
            kind: "screenshot",
          },
        ],
        videos: [
          {
            label: "手机侧实录：租户扫码后选择类型、填写档案并提交",
            webm: Buffer.from([0x1a, 0x45, 0xdf, 0xa3]),
          },
        ],
        startedAtSeconds: 12.3,
      },
    ],
    artifactLink: "房屋租赁合同-已签字盖章.pdf",
    artifactName: "房屋租赁合同-已签字盖章.pdf",
  };
}

test("教程 HTML：三段式、目录、章节跳转、录像与示例 PDF 链接齐全", () => {
  const html = renderTutorialHtml(minimalModel());

  // 三段式：每步都有「你要做什么 / 你会看到什么 / 注意什么」。
  expect(html).toContain("你要做什么");
  expect(html).toContain("你会看到什么");
  expect(html).toContain("注意什么");
  expect(html.match(/class="tri"/g)).toHaveLength(2);

  // 目录锚点到每一步；编号步骤按 1. 展示，准备步骤显示「准备」。
  expect(html).toContain('class="panel tut-toc"');
  expect(html).toContain('href="#s0"');
  expect(html).toContain('href="#s1"');
  expect(html).toContain("准备 开始前：确认 Word → PDF 转换器可用");
  expect(html).toContain("1. 导入 Word 合同模板");

  // 章节跳转按钮只在有录像时间点时渲染，秒数保留一位小数。
  expect(html.match(/class="video-seek"/g)).toHaveLength(1);
  expect(html).toContain('data-start="12.3"');
  expect(html).toContain("看本步视频（00:12）");

  // 录像以同目录相对引用，播放器带缺失兜底与四档速度按钮。
  expect(html).toContain('id="process-video"');
  expect(html).toContain(`src="${encodeURI(TUTORIAL_VIDEO_NAME)}"`);
  expect(html).toContain('id="tutorial-video-missing"');
  for (const rate of ["0.35", "0.5", "0.75", "1"]) {
    expect(html).toContain(`data-rate="${rate}"`);
  }

  // 结尾给出示例最终 PDF 的下载链接，步数不含「准备」。
  expect(html).toContain('class="pdf-link"');
  expect(html).toContain("打开本示例最终合同 PDF（房屋租赁合同-已签字盖章.pdf）");
  expect(html).toContain("走完这 1 步");

  // 教程是用户读物，不能混入测试元信息；分镜图以 base64 内嵌。
  expect(html).not.toContain("检查浏览器控制台");
  expect(html).toContain('src="data:image/png;base64,');

  // 手机侧录像：WebM base64 内嵌、中文标签、带播放控件（te-cfgvj）。
  expect(html.match(/class="step-videos"/g)).toHaveLength(1);
  expect(html).toContain('<video controls preload="metadata" src="data:video/webm;base64,');
  expect(html).toContain("手机侧实录：租户扫码后选择类型、填写档案并提交");
});

test("教程 HTML：手机侧录像缺失时标注原因，不渲染坏播放器", () => {
  const model = minimalModel();
  model.steps[1]!.videos = [
    {
      label: "手机侧实录：手机上打开签名页、手写三笔并提交",
      webm: null,
      warning: "该页面未启用录像（video 为 null）",
    },
  ];
  const html = renderTutorialHtml(model);
  expect(html).toContain("手机侧录像缺失：该页面未启用录像（video 为 null）");
  expect(html).toContain("手机侧实录：手机上打开签名页、手写三笔并提交");
  expect(html).not.toContain("data:video/webm");
});

test("业务步骤与教程文案一一对应，测试步骤不进教程", () => {
  const specPath = join(
    dirname(test.info().file),
    "business-acceptance.spec.ts",
  );
  const spec = readFileSync(specPath, "utf8");
  const actions = [...spec.matchAll(/action:\s*"([^"]+)"/g)].map(
    (match) => match[1],
  );
  expect(actions.length).toBeGreaterThan(0);

  // 每个业务步骤都必须有教程文案。
  for (const action of actions) {
    if (TEST_ONLY_ACTIONS.has(action)) {
      expect(
        tutorialCopyFor(action),
        `测试步骤不应有教程文案：${action}`,
      ).toBeUndefined();
      continue;
    }
    expect(
      TUTORIAL_COPY[action],
      `业务步骤缺少教程文案：${action}`,
    ).toBeDefined();
  }

  // 不允许出现业务步骤里已不存在的游离文案键。
  for (const key of Object.keys(TUTORIAL_COPY)) {
    expect(actions, `教程文案键在业务步骤中找不到：${key}`).toContain(key);
  }

  // 每段文案的三段式都不能为空。
  for (const [key, copy] of Object.entries(TUTORIAL_COPY)) {
    expect(copy.do.length, `${key} 缺少「你要做什么」`).toBeGreaterThan(0);
    expect(copy.see.length, `${key} 缺少「你会看到什么」`).toBeGreaterThan(0);
    expect(copy.note.length, `${key} 缺少「注意什么」`).toBeGreaterThan(0);
  }
});
