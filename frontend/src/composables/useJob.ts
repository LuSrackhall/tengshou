// 后台任务轮询组合式函数：启动 POST /jobs/*，按 400ms 轮询 GET /jobs/{id}
// 直到 done / failed。阶段、已用时间与真实 i/N 全部来自服务端快照，前端
// 不伪造百分比；超过 60 秒的安抚文案由 ProgressPanel 统一展示。
//
// 语义：
//   - 首次 GET 在读任务状态时立即触发（不空等一个轮询周期）；
//   - 组件卸载或调用 stop() 只停止轮询，不取消服务端任务（导入/生成继续
//     在后台完成，完成后各域按既有刷新链路取到结果）；
//   - 失败时抛 ApiError：code/message/fields 来自服务端错误信封，payload
//     是完整快照（Excel 逐行 import_errors 的 result 仍可从中取出）。

import { onBeforeUnmount, ref, type Ref } from "vue";
import { ApiError } from "../api/client";
import { getJob } from "../api/endpoints";
import type { JobSnapshot, JobStartResponse } from "../api/types";

/** 与需求一致的轮询间隔。 */
export const JOB_POLL_INTERVAL_MS = 400;

/** stop() 导致 start() 提前结束时抛出的客户端错误码。 */
const JOB_POLLING_STOPPED = "job_polling_stopped";

/** 轮询连续失败次数上限；超过后按网络错误抛出。 */
const JOB_POLL_ATTEMPTS = 3;

export interface JobRunner<T> {
  /** 最近一次快照；ProgressPanel 直接渲染。 */
  job: Ref<JobSnapshot<T> | null>;
  running: Ref<boolean>;
  /** 启动任务并轮询到结束；成功返回 result，失败抛出 ApiError。 */
  start(starter: () => Promise<JobStartResponse>): Promise<T>;
  /** 停止轮询（不取消服务端任务）。 */
  stop(): void;
}

/** 判断一个错误是否只是本地停止轮询（调用方应静默忽略）。 */
export function isJobCancelled(error: unknown): boolean {
  return error instanceof ApiError && error.code === JOB_POLLING_STOPPED;
}

function delay(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function jobFailure<T>(snapshot: JobSnapshot<T>): ApiError {
  const error = snapshot.error;
  return new ApiError({
    code: error?.code ?? "job_failed",
    message: error?.message ?? "任务失败，请重试",
    status: 0,
    fields: error?.fields ?? [],
    payload: snapshot,
  });
}

function stoppedError(): ApiError {
  return new ApiError({
    code: JOB_POLLING_STOPPED,
    message: "已停止等待后台任务",
    status: 0,
  });
}

export function useJob<T = unknown>(): JobRunner<T> {
  const job = ref<JobSnapshot<T> | null>(null) as Ref<JobSnapshot<T> | null>;
  const running = ref(false);
  // 每次 stop()/start() 递增；在途轮询据此作废，不会写回旧任务状态。
  let generation = 0;

  function stop(): void {
    generation += 1;
    running.value = false;
  }

  async function fetchSnapshot(id: string, run: number): Promise<JobSnapshot<T>> {
    let lastError: unknown = null;
    for (let attempt = 0; attempt < JOB_POLL_ATTEMPTS; attempt += 1) {
      if (run !== generation) {
        throw stoppedError();
      }
      try {
        return await getJob<T>(id);
      } catch (error) {
        lastError = error;
        // 任务不存在（过期/服务重启）不再重试，直接回显。
        if (error instanceof ApiError && error.status === 404) {
          throw error;
        }
        await delay(JOB_POLL_INTERVAL_MS);
      }
    }
    throw lastError;
  }

  async function start(starter: () => Promise<JobStartResponse>): Promise<T> {
    stop();
    const run = generation;
    running.value = true;
    job.value = null;
    try {
      const started = await starter();
      let snapshot = await fetchSnapshot(started.job_id, run);
      job.value = snapshot;
      while (snapshot.status === "running") {
        await delay(JOB_POLL_INTERVAL_MS);
        if (run !== generation) {
          throw stoppedError();
        }
        snapshot = await fetchSnapshot(started.job_id, run);
        job.value = snapshot;
      }
      if (snapshot.status === "failed") {
        throw jobFailure(snapshot);
      }
      return snapshot.result as T;
    } finally {
      if (run === generation) {
        running.value = false;
      }
    }
  }

  onBeforeUnmount(stop);

  return { job, running, start, stop };
}
