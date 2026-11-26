// 二维码会话对话框的共用接线：会话创建、2 秒状态轮询、多网卡地址选择、
// 关闭时取消未完成会话与二维码地址计算。合同签名（SignatureDialog）、档案签名
// （PartySignatureDialog）与自助建档（EnrollSessionDialog）只在端点、文案和
// 完成回调上不同，不再各自复制这套生命周期。
//
// 调用方在 onMounted 里调用 startSession() 即自动开始采集；onCompleted 返回
// 状态行文案与样式，其余副作用（toast、refresh、emit）由调用方在回调里完成。
import { computed, onBeforeUnmount, ref, type Ref } from "vue";
import { ApiError } from "../../api/client";
import { errorMessage, toast } from "../../bridge/app";

/** 三类会话视图的公共字段（SignatureSession / EnrollSession 均满足）。 */
export interface QrSessionItem {
  id: string;
  state: string;
  urls: string[];
  qr_url: string;
  signature_id?: string;
}

export interface QrSessionMessages {
  /** 等待手机提交时的状态文案。 */
  waiting: string;
  /** 会话不存在（服务重启/已取消）时的提示。 */
  lost: string;
  /** 二维码过期时的提示。 */
  expired: string;
}

export interface QrSessionCompletion {
  text: string;
  tone?: string;
}

export interface QrSessionOptions<T extends QrSessionItem> {
  /** 创建会话并返回会话视图（端点返回的 item）。 */
  create: () => Promise<T>;
  /** 轮询会话状态。 */
  get: (id: string) => Promise<T>;
  /** 关闭时取消未完成会话；失败可忽略。 */
  cancel: (id: string) => Promise<unknown>;
  /** 二维码 PNG 地址（多网卡切换用）。 */
  qrUrl: (id: string, index: number) => string;
  messages: QrSessionMessages;
  /** 会话创建成功后的回调（例如展示有效期）。 */
  onStarted?: (item: T) => void;
  /** 轮询到 completed 时调用；返回状态行文案与样式。 */
  onCompleted?: (item: T) => QrSessionCompletion | void;
}

export function useQrSession<T extends QrSessionItem>(
  options: QrSessionOptions<T>,
) {
  const session = ref(null) as Ref<T | null>;
  const starting = ref(false);
  const startError = ref("");
  const stateText = ref(options.messages.waiting);
  const stateClass = ref("");
  /** 多网卡时用户选择的地址下标；null 表示还未手动选择。 */
  const qrIndex = ref<number | null>(null);

  let pollTimer: ReturnType<typeof setInterval> | null = null;
  let activeId: string | null = null;
  let closed = false;

  const urls = computed(() => session.value?.urls ?? []);
  const currentUrl = computed(() => {
    const list = urls.value;
    return list[qrIndex.value ?? 0] || list[0] || "";
  });
  const qrSrc = computed(() => {
    const item = session.value;
    if (!item) {
      return "";
    }
    return qrIndex.value === null
      ? item.qr_url
      : options.qrUrl(item.id, qrIndex.value);
  });

  function stopPolling(): void {
    if (pollTimer !== null) {
      clearInterval(pollTimer);
      pollTimer = null;
    }
  }

  function cancelActive(): void {
    if (!activeId) {
      return;
    }
    const id = activeId;
    activeId = null;
    options.cancel(id).catch(() => {
      /* 会话可能已完成或已过期：取消失败不影响界面 */
    });
  }

  function finishWithMessage(message: string, cls: string): void {
    stopPolling();
    activeId = null;
    stateText.value = message;
    stateClass.value = cls;
  }

  function pollSession(id: string): void {
    stopPolling();
    activeId = id;
    const tick = async (): Promise<void> => {
      if (activeId !== id) {
        stopPolling();
        return;
      }
      let item: T;
      try {
        item = await options.get(id);
      } catch (error) {
        if (activeId !== id) {
          return;
        }
        if (error instanceof ApiError && error.code === "not_found") {
          finishWithMessage(options.messages.lost, "error");
        }
        return; // 其余错误是瞬时的：继续轮询
      }
      if (activeId !== id) {
        return;
      }
      if (item.state === "completed") {
        stopPolling();
        activeId = null;
        const done = options.onCompleted?.(item);
        if (done) {
          stateText.value = done.text;
          stateClass.value = done.tone ?? "ok";
        }
      } else if (item.state === "expired") {
        finishWithMessage(options.messages.expired, "error");
      }
    };
    void tick();
    pollTimer = setInterval(() => {
      void tick();
    }, 2000);
  }

  async function startSession(): Promise<void> {
    stopPolling();
    cancelActive();
    startError.value = "";
    session.value = null;
    qrIndex.value = null;
    stateText.value = options.messages.waiting;
    stateClass.value = "";
    starting.value = true;
    try {
      const item = await options.create();
      if (closed) {
        options.cancel(item.id).catch(() => {
          /* 已关闭：尽力取消，不更新界面 */
        });
        return;
      }
      activeId = item.id;
      session.value = item;
      options.onStarted?.(item);
      pollSession(item.id);
    } catch (error) {
      activeId = null;
      startError.value = errorMessage(error);
      toast(errorMessage(error), true);
    } finally {
      starting.value = false;
    }
  }

  onBeforeUnmount(() => {
    closed = true;
    stopPolling();
    cancelActive();
  });

  return {
    session,
    starting,
    startError,
    stateText,
    stateClass,
    qrIndex,
    urls,
    currentUrl,
    qrSrc,
    startSession,
  };
}
