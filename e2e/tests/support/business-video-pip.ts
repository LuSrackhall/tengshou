// 手机侧录像 → 主录像画中画合成（te-fp48w）。
//
// 背景：报告/教程里的「手机侧实录」播放器只有在打开 HTML 时才能看到；用户直接
// 看主录像文件（验收过程录像.webm）或点步骤里的「▶ 看本步过程」时，画面里没有
// 手机上发生的过程。这里在测试结束后由报告器调用本机 ffmpeg，把手机页录像等比
// 缩到画面宽 1/3、右下角、绿色细边框，按手机页创建/关闭的墙钟窗口叠进主录像，
// 主录像时间轴不变（HTML 的 data-start 不动）。
//
// 依赖与降级：只用 PATH 里的系统 ffmpeg（Playwright 自带的 ffmpeg 没有 overlay
// 滤镜）。ffmpeg 不可用或合成失败时跳过画中画并在报告视频区给出提示，步骤内嵌
// 手机侧录像不受影响，E2E 结论不变。可用 E2E_FFMPEG 指定路径；E2E_FFMPEG=off
// 显式禁用（用于本地验证降级路径）。
//
// 合成/计划读取/清理都是同步函数：报告器钩子（report-video-reporter.ts）必须
// 在测试进程退出前完成合成，而 Playwright 不会等待异步报告器钩子。
import { execFileSync, spawnSync } from "node:child_process";
import {
  copyFileSync,
  existsSync,
  mkdirSync,
  readFileSync,
  rmSync,
  statSync,
} from "node:fs";
import { mkdir, writeFile } from "node:fs/promises";
import { join } from "node:path";

/** 报告目录下的隐藏暂存目录：合成完成后由报告器整体删除，交付件仍只有 4 个。 */
export const PIP_DIR_NAME = ".pip";
export const PIP_PLAN_NAME = "plan.json";
export const PIP_PLAN_SCHEMA = "tengshou.e2e.video-pip.v1";

/** 一段手机侧录像在主录像时间轴上的位置。 */
export interface PipSegment {
  /** 中文标签（与步骤内嵌播放器一致）。 */
  label: string;
  /** 手机页开始录制（page 创建）相对报告起点的秒数。 */
  startedAtSeconds: number;
  /** 手机页录制的墙钟时长（秒）。 */
  durationSeconds: number;
  /** 手机侧 WebM 的绝对路径（报告目录 .pip 下）。 */
  path: string;
}

export interface PipPlan {
  schema: string;
  segments: PipSegment[];
}

export interface FfmpegProbe {
  /** 可执行的 ffmpeg 命令/路径；null 表示不可用。 */
  path: string | null;
  /** 不可用原因（path 为 null 时给出中文说明）。 */
  reason?: string;
}

export interface PipCompositeResult {
  /** 实际合成的手机侧录像段数。 */
  applied: number;
  /** 降级/失败原因；成功时为 undefined。 */
  warning?: string;
}

const PIP_WIDTH = 480; // 1440 画面宽的 1/3
const PIP_MARGIN = 24;
const PIP_BORDER = 3;
const PIP_BORDER_COLOR = "0x1f6f4a";
const PIP_MIN_SECONDS = 0.5;

/** 探测系统 ffmpeg：优先 E2E_FFMPEG，其次 PATH；不碰 Playwright 自带的 ffmpeg。 */
export function probeFfmpeg(): FfmpegProbe {
  const override = (process.env.E2E_FFMPEG ?? "").trim();
  if (override === "off") {
    return { path: null, reason: "E2E_FFMPEG=off（显式禁用画中画合成）" };
  }
  if (override) {
    const probe = spawnSync(override, ["-version"], { stdio: "ignore" });
    if (probe.status === 0) {
      return { path: override };
    }
    return {
      path: null,
      reason: `E2E_FFMPEG 指定的 ffmpeg 不可用：${override}`,
    };
  }
  const probe = spawnSync("ffmpeg", ["-version"], { stdio: "ignore" });
  if (probe.status === 0) {
    return { path: "ffmpeg" };
  }
  return {
    path: null,
    reason:
      "PATH 中未找到 ffmpeg（Playwright 自带的 ffmpeg 没有 overlay 滤镜，不能用于合成）",
  };
}

/** 合成本身是否值得执行：窗口有效且文件存在。 */
function usableSegments(segments: PipSegment[]): PipSegment[] {
  return segments.filter(
    (segment) =>
      Number.isFinite(segment.startedAtSeconds) &&
      Number.isFinite(segment.durationSeconds) &&
      segment.startedAtSeconds >= 0 &&
      segment.durationSeconds >= PIP_MIN_SECONDS &&
      typeof segment.path === "string" &&
      segment.path !== "" &&
      existsSync(segment.path),
  );
}

export interface PipFilterPlan {
  segments: PipSegment[];
  filterComplex: string;
  /** filter_complex 的最终视频标签（含方括号）。 */
  outputLabel: string;
}

/**
 * 组装 overlay 滤镜：每段手机录像用 setpts 平移到窗口起点，缩到宽 480 并加绿色
 * 细边框，右下角叠在主录像上；enable 只在该窗口内合成，主录像时间轴不变。
 */
export function buildPipFilterPlan(
  segments: PipSegment[],
): PipFilterPlan | null {
  const usable = usableSegments(segments);
  if (usable.length === 0) {
    return null;
  }
  const parts: string[] = [];
  let previous = "[0:v]";
  usable.forEach((segment, index) => {
    const start = Math.max(0, segment.startedAtSeconds);
    const end = start + Math.max(PIP_MIN_SECONDS, segment.durationSeconds);
    const pip = `pip${index}`;
    parts.push(
      `[${index + 1}:v]setpts=PTS-STARTPTS+${start.toFixed(3)}/TB,` +
        `scale=${PIP_WIDTH}:-2,` +
        `pad=iw+${PIP_BORDER * 2}:ih+${PIP_BORDER * 2}:${PIP_BORDER}:${PIP_BORDER}:color=${PIP_BORDER_COLOR}` +
        `[${pip}]`,
    );
    const next = `v${index}`;
    parts.push(
      `${previous}[${pip}]overlay=` +
        `x=W-w-${PIP_MARGIN}:y=H-h-${PIP_MARGIN}:` +
        `enable='between(t,${start.toFixed(3)},${end.toFixed(3)})'` +
        `[${next}]`,
    );
    previous = `[${next}]`;
  });
  return {
    segments: usable,
    filterComplex: parts.join(";"),
    outputLabel: previous,
  };
}

/** ffmpeg 参数：输入主录像 → 依次叠加手机侧录像 → 仍编码为 VP9 WebM（无音频）。 */
export function buildFfmpegArgs(
  mainVideoPath: string,
  plan: PipFilterPlan,
  outPath: string,
): string[] {
  const args = ["-hide_banner", "-loglevel", "error", "-y", "-i", mainVideoPath];
  for (const segment of plan.segments) {
    args.push("-i", segment.path);
  }
  args.push(
    "-filter_complex",
    plan.filterComplex,
    "-map",
    plan.outputLabel,
    "-an",
    "-c:v",
    "libvpx-vp9",
    "-b:v",
    "0",
    "-crf",
    "32",
    "-row-mt",
    "1",
    "-deadline",
    "realtime",
    "-cpu-used",
    "8",
    "-pix_fmt",
    "yuv420p",
    outPath,
  );
  return args;
}

function errorSummary(error: unknown): string {
  if (error instanceof Error) {
    return error.message.split("\n")[0] ?? error.message;
  }
  return String(error);
}

/**
 * 把手机侧录像合成进主录像文件（原地替换）。任何失败都只返回 warning，由调用方
 * 决定是否写入报告提示；绝不抛出（报告器里出错会顶掉测试结论）。
 *
 * 同步实现是必须的：Playwright 的报告器钩子不会被 await——它在钩子返回的
 * Promise 完成前就结束了进程。ffmpeg 合成要跑几十秒，用异步实现时进程先退出，
 * 临时文件留在 `.pip`、主录像永远合不上（te-fp48w 实测）。同步阻塞钩子能保证
 * 合成、替换和清理都在测试进程退出前完成。
 */
export function compositePip(
  ffmpegPath: string,
  mainVideoPath: string,
  segments: PipSegment[],
  workDir: string,
  timeoutMs = 20 * 60_000,
): PipCompositeResult {
  const plan = buildPipFilterPlan(segments);
  if (!plan) {
    return { applied: 0, warning: "没有可合成的手机侧录像（窗口或文件无效）" };
  }
  const pipDir = join(workDir, PIP_DIR_NAME);
  const tempPath = join(
    pipDir,
    `composite-${process.pid}-${Date.now()}.webm`,
  );
  try {
    mkdirSync(pipDir, { recursive: true });
    try {
      execFileSync(
        ffmpegPath,
        buildFfmpegArgs(mainVideoPath, plan, tempPath),
        { timeout: timeoutMs, stdio: "pipe", maxBuffer: 16 * 1024 * 1024 },
      );
    } catch (error) {
      const execError = error as { stderr?: Buffer | string };
      const stderr = String(execError.stderr ?? "")
        .trim()
        .split("\n")
        .slice(-3)
        .join(" ");
      throw new Error(
        `${ffmpegPath} 执行失败：${stderr || errorSummary(error)}`,
      );
    }
    const info = statSync(tempPath);
    if (info.size <= 0) {
      throw new Error("ffmpeg 输出为空文件");
    }
    // 先复制成功产物再删原文件：copyFileSync 覆盖 Windows 上的已存在文件也安全。
    copyFileSync(tempPath, mainVideoPath);
    return { applied: plan.segments.length };
  } catch (error) {
    return {
      applied: 0,
      warning: `手机侧画中画合成失败（主录像保持原样）：${errorSummary(error)}`,
    };
  } finally {
    try {
      rmSync(tempPath, { force: true });
    } catch {
      // 清理失败不影响合成结果。
    }
  }
}

export async function writePipPlan(
  outputDir: string,
  segments: PipSegment[],
): Promise<void> {
  const pipDir = join(outputDir, PIP_DIR_NAME);
  await mkdir(pipDir, { recursive: true });
  const plan: PipPlan = { schema: PIP_PLAN_SCHEMA, segments };
  await writeFile(
    join(pipDir, PIP_PLAN_NAME),
    `${JSON.stringify(plan, null, 2)}\n`,
    "utf8",
  );
}

/** 读取报告器要用的合成计划；缺失/损坏都返回 null（视为没有需要合成的段）。 */
export function readPipPlan(outputDir: string): PipPlan | null {
  try {
    const raw = readFileSync(
      join(outputDir, PIP_DIR_NAME, PIP_PLAN_NAME),
      "utf8",
    );
    const parsed = JSON.parse(raw) as Partial<PipPlan>;
    if (parsed.schema !== PIP_PLAN_SCHEMA || !Array.isArray(parsed.segments)) {
      return null;
    }
    return { schema: PIP_PLAN_SCHEMA, segments: parsed.segments };
  } catch {
    return null;
  }
}

/** 删除 .pip 暂存目录：无论合成成功与否，报告目录都只留交付文件。 */
export function cleanupPipDir(outputDir: string): void {
  rmSync(join(outputDir, PIP_DIR_NAME), {
    recursive: true,
    force: true,
  });
}
