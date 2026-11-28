// 手机侧录像画中画合成的常驻断言（te-fp48w，纯 Node，不启动浏览器）。
//
// 覆盖三件事：
// 1) overlay 滤镜计划：窗口平移（setpts）、等比缩放、绿色细边框、右下角与
//    enable 时间窗；无效窗口被丢弃；
// 2) 合成计划读写与 .pip 暂存目录清理（报告目录只留交付件）；
// 3) 本机有系统 ffmpeg 时做一次真实合成：窗口内右下角出现手机画面，窗口外
//    保持主画面；没有 ffmpeg（或 E2E_FFMPEG 指向不可用路径）时跳过，不影响
//    E2E 结论——降级路径由报告器的 warning 覆盖。
import { execFileSync } from "node:child_process";
import { existsSync } from "node:fs";
import { mkdir, mkdtemp, readFile, rm, stat, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { expect, test } from "@playwright/test";
import type {
  FullConfig,
  TestCase,
  TestResult,
} from "@playwright/test/reporter";
import ProcessVideoReporter from "./support/report-video-reporter";
import { nextPhoneVideoPath } from "./support/business-frames";
import {
  PIP_DIR_NAME,
  PIP_PLAN_NAME,
  buildFfmpegArgs,
  buildPipFilterPlan,
  cleanupPipDir,
  compositePip,
  probeFfmpeg,
  readPipPlan,
  writePipPlan,
  type FfmpegProbe,
  type PipSegment,
} from "./support/business-video-pip";

const SEGMENT: PipSegment = {
  label: "手机侧实录：租户扫码后选择类型、填写档案并提交",
  startedAtSeconds: 12.3,
  durationSeconds: 9.5,
  path: "/tmp/phone.webm",
};

function withFfmpegEnv<T>(value: string, fn: () => T): T {
  const previous = process.env.E2E_FFMPEG;
  process.env.E2E_FFMPEG = value;
  try {
    return fn();
  } finally {
    if (previous === undefined) {
      delete process.env.E2E_FFMPEG;
    } else {
      process.env.E2E_FFMPEG = previous;
    }
  }
}

test("画中画滤镜：窗口内平移缩放、右下角细边框，主录像时间轴不变", () => {
  const second: PipSegment = {
    ...SEGMENT,
    label: "手机侧实录：手机上打开签名页、手写三笔并提交",
    startedAtSeconds: 40.25,
    durationSeconds: 8,
    path: "/tmp/sign.webm",
  };
  // 两个段都要通过文件存在检查；用测试自身路径占位。
  const existing: PipSegment[] = [SEGMENT, second].map((segment) => ({
    ...segment,
    path: test.info().file,
  }));
  const plan = buildPipFilterPlan(existing);
  expect(plan).not.toBeNull();
  expect(plan!.segments).toHaveLength(2);
  // 第一段窗口 [12.300, 21.800]，第二段 [40.250, 48.250]。
  expect(plan!.filterComplex).toContain("setpts=PTS-STARTPTS+12.300/TB");
  expect(plan!.filterComplex).toContain("setpts=PTS-STARTPTS+40.250/TB");
  expect(plan!.filterComplex).toContain("scale=480:-2");
  expect(plan!.filterComplex).toContain("pad=iw+6:ih+6:3:3:color=0x1f6f4a");
  expect(plan!.filterComplex).toContain("x=W-w-24:y=H-h-24");
  expect(plan!.filterComplex).toContain(
    "enable='between(t,12.300,21.800)'",
  );
  expect(plan!.filterComplex).toContain(
    "enable='between(t,40.250,48.250)'",
  );
  expect(plan!.outputLabel).toBe("[v1]");
  // 输入按段顺序排在主录像之后。
  const args = buildFfmpegArgs("/tmp/main.webm", plan!, "/tmp/out.webm");
  expect(args.slice(0, 6)).toEqual([
    "-hide_banner", "-loglevel", "error", "-y", "-i", "/tmp/main.webm",
  ]);
  expect(args).toContain("libvpx-vp9");
  expect(args[args.length - 1]).toBe("/tmp/out.webm");
});

test("画中画滤镜：无效窗口（过短/负数/文件不存在）不参与合成", () => {
  const invalid: PipSegment[] = [
    { ...SEGMENT, path: test.info().file, durationSeconds: 0.1 },
    { ...SEGMENT, path: test.info().file, startedAtSeconds: -1 },
    { ...SEGMENT, path: "/definitely/not/here.webm" },
  ];
  expect(buildPipFilterPlan([])).toBeNull();
  expect(buildPipFilterPlan(invalid)).toBeNull();
});

test("合成计划：读写往返，清理 .pip 暂存目录", async () => {
  const dir = await mkdtemp(join(tmpdir(), "pip-plan-"));
  try {
    await writePipPlan(dir, [SEGMENT]);
    const plan = readPipPlan(dir);
    expect(plan?.segments).toHaveLength(1);
    expect(plan?.segments[0]?.label).toBe(SEGMENT.label);
    const raw = await readFile(
      join(dir, PIP_DIR_NAME, PIP_PLAN_NAME),
      "utf8",
    );
    expect(raw).toContain("tengshou.e2e.video-pip.v1");
    cleanupPipDir(dir);
    expect(readPipPlan(dir)).toBeNull();
  } finally {
    await rm(dir, { recursive: true, force: true });
  }
});

test("手机侧暂存录像：跨步骤（不同 FrameRecorder）文件名不冲突", () => {
  // runStep 每个业务步骤新建一个 FrameRecorder；两段手机录像必须写到
  // 不同文件，否则第二段的 saveAs 会覆盖第一段，画中画两段都指向同一段。
  const a = nextPhoneVideoPath("/tmp/report/.pip");
  const b = nextPhoneVideoPath("/tmp/report/.pip");
  expect(a).not.toBe(b);
  expect(a.endsWith(".webm")).toBe(true);
  expect(b.endsWith(".webm")).toBe(true);
});

test("合成计划：缺失或损坏时按「无需合成」处理", async () => {
  const dir = await mkdtemp(join(tmpdir(), "pip-broken-"));
  try {
    expect(readPipPlan(dir)).toBeNull();
    await mkdir(join(dir, PIP_DIR_NAME), { recursive: true });
    await writeFile(join(dir, PIP_DIR_NAME, PIP_PLAN_NAME), "not json", "utf8");
    expect(readPipPlan(dir)).toBeNull();
    await writeFile(
      join(dir, PIP_DIR_NAME, PIP_PLAN_NAME),
      JSON.stringify({ schema: "other", segments: [] }),
      "utf8",
    );
    expect(readPipPlan(dir)).toBeNull();
  } finally {
    await rm(dir, { recursive: true, force: true });
  }
});

test("ffmpeg 探测：E2E_FFMPEG 显式关闭/路径不可用时降级", () => {
  expect(withFfmpegEnv("off", probeFfmpeg).path).toBeNull();
  const missing = withFfmpegEnv("/definitely/not/ffmpeg", probeFfmpeg);
  expect(missing.path).toBeNull();
  expect(missing.reason).toContain("/definitely/not/ffmpeg");
});

test("真实合成：窗口内右下角出现手机画面，窗口外保持主画面", async () => {
  const probe: FfmpegProbe = probeFfmpeg();
  test.skip(probe.path === null, `本机没有可用 ffmpeg：${probe.reason}`);
  const ffmpeg = probe.path!;
  const dir = await mkdtemp(join(tmpdir(), "pip-composite-"));
  const mainPath = join(dir, "main.webm");
  const phonePath = join(dir, "phone.webm");
  // 小尺寸合成：滤镜与真实录像同构（固定 480 宽画中画在 640 宽主画面上仍完整）。
  const encode = (args: string[]) =>
    execFileSync(ffmpeg, args, { stdio: "ignore", timeout: 120_000 });
  try {
    encode([
      "-hide_banner", "-loglevel", "error", "-y",
      "-f", "lavfi", "-i", "color=c=0x336699:s=640x480:d=2:r=25",
      "-c:v", "libvpx-vp9", "-b:v", "0", "-crf", "30",
      "-deadline", "realtime", "-cpu-used", "8", "-pix_fmt", "yuv420p",
      mainPath,
    ]);
    encode([
      "-hide_banner", "-loglevel", "error", "-y",
      "-f", "lavfi", "-i", "color=c=0xff0000:s=640x480:d=1:r=25",
      "-c:v", "libvpx-vp9", "-b:v", "0", "-crf", "30",
      "-deadline", "realtime", "-cpu-used", "8", "-pix_fmt", "yuv420p",
      phonePath,
    ]);
    const result = compositePip(
      ffmpeg,
      mainPath,
      [
        {
          label: "手机侧实录：测试",
          startedAtSeconds: 0.5,
          durationSeconds: 1,
          path: phonePath,
        },
      ],
      dir,
    );
    expect(result.warning).toBeUndefined();
    expect(result.applied).toBe(1);
    expect((await stat(mainPath)).size).toBeGreaterThan(0);
    const pixel = (seconds: string) =>
      execFileSync(
        ffmpeg,
        [
          "-hide_banner", "-loglevel", "error", "-ss", seconds,
          "-i", mainPath, "-frames:v", "1",
          "-vf", "crop=1:1:600:440,format=rgb24",
          "-f", "rawvideo", "-",
        ],
        { timeout: 60_000 },
      );
    // 画中画区域：x 600 / y 440 落在右下角手机画面内（红色）。
    const inside = pixel("1.2");
    expect(inside[0]).toBeGreaterThan(180);
    expect(inside[1]).toBeLessThan(80);
    expect(inside[2]).toBeLessThan(80);
    // 窗口之外同一位置是主画面（蓝色 0x336699，绿色通道明显高于红色）。
    const outside = pixel("0.2");
    expect(outside[1]).toBeGreaterThan(outside[0]);
  } finally {
    await rm(dir, { recursive: true, force: true });
  }
});

test("报告器 onTestEnd：返回时已完成合成、补全 HTML 并清理 .pip", async () => {
  // Playwright 不等待异步报告器钩子：进程会在 ffmpeg 合成完成前退出。这个
  // 用例直接把钩子当普通同步函数调用，断言它返回时一切已完成——如果哪天有人
  // 把 onTestEnd 改回 async，这里不会失败，但验收 E2E 的主录像会再次没有
  // 画中画（真实回归由 business-acceptance 验收覆盖）。
  const probe: FfmpegProbe = probeFfmpeg();
  test.skip(probe.path === null, `本机没有可用 ffmpeg：${probe.reason}`);
  const ffmpeg = probe.path!;
  const dir = await mkdtemp(join(tmpdir(), "pip-reporter-"));
  const attachmentPath = join(dir, "playwright-recording.webm");
  const videoName = "验收过程录像.webm";
  const reportName = "业务验收报告.html";
  const encode = (args: string[]) =>
    execFileSync(ffmpeg, args, { stdio: "ignore", timeout: 120_000 });
  try {
    encode([
      "-hide_banner", "-loglevel", "error", "-y",
      "-f", "lavfi", "-i", "color=c=0x336699:s=640x480:d=2:r=25",
      "-c:v", "libvpx-vp9", "-b:v", "0", "-crf", "30",
      "-deadline", "realtime", "-cpu-used", "8", "-pix_fmt", "yuv420p",
      attachmentPath,
    ]);
    const phonePath = join(dir, PIP_DIR_NAME, "phone-test.webm");
    await mkdir(join(dir, PIP_DIR_NAME), { recursive: true });
    encode([
      "-hide_banner", "-loglevel", "error", "-y",
      "-f", "lavfi", "-i", "color=c=0xff0000:s=640x480:d=1:r=25",
      "-c:v", "libvpx-vp9", "-b:v", "0", "-crf", "30",
      "-deadline", "realtime", "-cpu-used", "8", "-pix_fmt", "yuv420p",
      phonePath,
    ]);
    await writePipPlan(dir, [
      {
        label: "手机侧实录：测试",
        startedAtSeconds: 0.5,
        durationSeconds: 1,
        path: phonePath,
      },
    ]);
    await writeFile(
      join(dir, reportName),
      '<html data-video="0"><!--PROCESS_VIDEO_START-->' +
        "<p>占位</p>" +
        "<!--PROCESS_VIDEO_END--></html>",
      "utf8",
    );
    const reporter = new ProcessVideoReporter({
      outputDir: dir,
      reportName,
      videoName,
    });
    reporter.onBegin({
      configFile: join(dir, "playwright.config.ts"),
    } as unknown as FullConfig);
    reporter.onTestEnd(
      {
        location: { file: "/tmp/business-acceptance.spec.ts" },
      } as unknown as TestCase,
      {
        attachments: [
          { name: "video", contentType: "video/webm", path: attachmentPath },
        ],
      } as unknown as TestResult,
    );
    // 同步返回即代表完成：报告写明已合成、占位已替换、.pip 已删除。
    const html = await readFile(join(dir, reportName), "utf8");
    expect(html).toContain('data-video="1"');
    expect(html).toContain("已按所属步骤时间合成在录像右下角");
    expect(existsSync(join(dir, PIP_DIR_NAME))).toBe(false);
    // 主录像确实带上了画中画：窗口内右下角是红色手机画面。
    const videoPath = join(dir, videoName);
    const inside = execFileSync(
      ffmpeg,
      [
        "-hide_banner", "-loglevel", "error", "-ss", "1.2",
        "-i", videoPath, "-frames:v", "1",
        "-vf", "crop=1:1:600:440,format=rgb24",
        "-f", "rawvideo", "-",
      ],
      { timeout: 60_000 },
    );
    expect(inside[0]).toBeGreaterThan(180);
    expect(inside[1]).toBeLessThan(80);
    expect(inside[2]).toBeLessThan(80);
  } finally {
    await rm(dir, { recursive: true, force: true });
  }
});
