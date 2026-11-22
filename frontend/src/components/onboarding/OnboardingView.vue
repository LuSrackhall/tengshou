<script setup lang="ts">
// 新手引导（阶段 1 迁出 app.js 的 renderOnboarding / handleOnboardingStep /
// setOnboardingAction 与 index.html 里的静态骨架）。
//
// 五步引导、完成 / 跳过 / 重新打开、步骤深链（模板导入/标定、合同新建）、
// 本机自检与模板体检都在这里渲染；完成判定、步骤顺序、首装判定与跳过语义
// 仍由服务端决定，本组件不改写。
import { computed } from "vue";
import SelfCheckPanel from "./SelfCheckPanel.vue";
import TemplateCheckPanel from "./TemplateCheckPanel.vue";
import { bannerText, progressText } from "./format";
import { useOnboarding } from "./useOnboarding";

const {
  onboarding,
  guideLoaded,
  guideError,
  steps,
  showSkip,
  showComplete,
  showReopen,
  check,
  checkState,
  checkError,
  templateCheck,
  templateCheckState,
  templateCheckError,
  templateCheckName,
  templateCheckVersion,
  start,
  handleStep,
  recheck,
  refreshTemplateCheck,
  complete,
  skip,
  reopen,
  goFields,
  goCalibrate,
  openCheckTarget,
} = useOnboarding();

const progress = computed(() =>
  onboarding.value ? progressText(onboarding.value) : "",
);

const banner = computed(() =>
  onboarding.value ? bannerText(onboarding.value) : "",
);

const checkSummary = computed(() => {
  const summary = check.value?.summary;
  return summary
    ? `通过 ${summary.ok} · 需留意 ${summary.warn} · 需处理 ${summary.error}`
    : "";
});
</script>

<template>
  <div class="onboard-hero">
    <div class="onboard-hero-copy">
      <span class="onboard-kicker">第一次使用</span>
      <h2>从您手上的模板开始</h2>
      <p>腾手不预置任何地方模板：导入上级或对方给过的 Word 模板（.docx）或现成 PDF，标好位置，就能生成合同。整个引导在本机完成，不需要联网。</p>
      <div class="onboard-cta">
        <button type="button" id="onboardStartBtn" class="primary" @click="start">我有一份上级模板</button>
        <button type="button" id="onboardRecheckBtn" @click="recheck">运行自检</button>
        <button v-show="showSkip" type="button" id="onboardSkipBtn" @click="skip">暂时跳过</button>
      </div>
      <p class="onboard-banner" id="onboardBanner">{{ banner }}</p>
    </div>
    <div class="onboard-trial-note">
      <strong>样例数据，不写入台账</strong>
      <p>引导里的样例预览与试生成只使用示例数据：不会写入合同、档案、模板版本或生成记录。看懂了再录真实合同。</p>
    </div>
  </div>
  <div class="onboard-grid">
    <div class="onboard-card">
      <div class="onboard-card-head">
        <h3>五步开始</h3>
        <span class="muted" id="onboardProgress">{{ progress }}</span>
      </div>
      <div id="onboardSteps" class="onboard-steps">
        <p v-if="!guideLoaded" class="hint">正在读取引导状态…</p>
        <div v-else-if="guideError" class="form-errors">引导状态读取失败：{{ guideError }}</div>
        <template v-else>
          <button
            v-for="(step, index) in steps"
            :key="step.id"
            type="button"
            class="onboard-step"
            :class="{ done: step.done }"
            :data-step="step.action"
            @click="handleStep(step.action)"
          >
            <span class="onboard-step-no">{{ step.done ? "✓" : index + 1 }}</span>
            <span class="onboard-step-copy">
              <strong>{{ step.label }}</strong>
              <span>{{ step.detail || "" }}</span>
            </span>
            <span class="badge" :class="step.done ? 'active' : 'draft'">{{ step.done ? "已完成" : "待完成" }}</span>
          </button>
        </template>
      </div>
      <div class="onboard-actions">
        <button v-show="showComplete" type="button" id="onboardCompleteBtn" @click="complete">我已完成引导</button>
        <button v-show="showReopen" type="button" id="onboardReopenBtn" @click="reopen">重新打开引导</button>
      </div>
    </div>
    <div class="onboard-card">
      <div class="onboard-card-head">
        <h3>本机自检</h3>
        <span class="muted" id="onboardCheckSummary">{{ checkSummary }}</span>
      </div>
      <SelfCheckPanel
        :state="checkState"
        :data="check"
        :error="checkError"
        @open-tab="openCheckTarget"
      />
      <div class="onboard-card-head onboard-template-head">
        <h3>模板体检</h3>
        <button type="button" id="onboardTemplateCheckBtn" @click="refreshTemplateCheck">重新检查</button>
      </div>
      <TemplateCheckPanel
        :state="templateCheckState"
        :data="templateCheck"
        :error="templateCheckError"
        :template-name="templateCheckName"
        :template-version="templateCheckVersion"
        @go-fields="goFields"
        @go-calibrate="goCalibrate"
      />
    </div>
  </div>
  <p class="hint" id="onboardFooter">引导只做提示与校验，不改变您原有的模板、台账和生成流程；随时可从顶部导航重新打开。</p>
</template>
