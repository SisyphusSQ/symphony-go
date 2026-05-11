<template>
  <section class="status-bar" aria-label="Operator status">
    <div class="status-card lifecycle-card">
      <div class="metric-label">Lifecycle</div>
      <div class="lifecycle-row">
        <a-badge :status="lifecycleTone" :text="state?.lifecycle.state || 'unknown'" />
        <a-tag :color="source === 'api' ? 'blue' : 'gold'">{{ sourceLabel }}</a-tag>
      </div>
      <div class="metric-note">{{ readyText }}</div>
    </div>

    <div class="status-card">
      <a-statistic title="Running" :value="count('running')" :loading="loading" />
      <div class="metric-note">active sessions</div>
    </div>
    <div class="status-card">
      <a-statistic title="Retrying" :value="count('retrying')" :loading="loading" />
      <div class="metric-note">backoff queue</div>
    </div>
    <div class="status-card">
      <a-statistic title="Failed" :value="count('failed')" :loading="loading" />
      <div class="metric-note">needs review</div>
    </div>
    <div class="status-card">
      <a-statistic title="Tokens" :value="totalTokens" :loading="loading" />
      <div class="metric-note">all visible runs</div>
    </div>
    <div class="status-card">
      <a-statistic title="Runtime" :value="runtimeText" :loading="loading" />
      <div class="metric-note">aggregate</div>
    </div>
    <div class="status-card rate-limit-card">
      <div class="metric-label">Rate Limit</div>
      <div class="rate-limit-value">{{ rateLimitText }}</div>
      <a-progress
        :percent="rateLimitPercent"
        :show-info="false"
        :stroke-color="rateLimitColor"
        size="small"
      />
      <div class="metric-note">{{ rateLimitNote }}</div>
    </div>

  </section>
</template>

<script setup lang="ts">
import { computed } from "vue";

import { formatCount, formatRuntime, statusTone } from "../api/format";
import type { OperatorDataSource, StateResponse } from "../api/operator";

const props = defineProps<{
  state: StateResponse | null;
  loading: boolean;
  source: OperatorDataSource;
  fallbackReason: string;
}>();

const sourceLabel = computed(() => (props.source === "api" ? "API" : "Mock"));

const lifecycleTone = computed(() => {
  const state = props.state?.lifecycle.state || "";
  if (state === "running") {
    return "processing";
  }
  return statusTone(state);
});

const readyText = computed(() => {
  if (!props.state) {
    return "No state loaded";
  }
  if (props.state.ready.ok) {
    return "Dispatch ready";
  }
  return props.state.ready.error || "Dispatch not ready";
});

const totalTokens = computed(() => formatCount(props.state?.tokens.total_tokens));
const runtimeText = computed(() => formatRuntime(props.state?.runtime.total_seconds));
const rateLimit = computed(() => {
  const latest = props.state?.rate_limit.latest;
  if (!latest || typeof latest !== "object") {
    return {};
  }
  return latest as { primary_remaining?: number; reset_seconds?: number };
});
const rateLimitText = computed(() => {
  if (rateLimit.value.primary_remaining === undefined) {
    return "-";
  }
  return formatCount(rateLimit.value.primary_remaining);
});
const rateLimitPercent = computed(() => {
  if (rateLimit.value.primary_remaining === undefined) {
    return 0;
  }
  return Math.min(100, Math.round((rateLimit.value.primary_remaining / 5000) * 100));
});
const rateLimitColor = computed(() => {
  if (rateLimitPercent.value < 20) {
    return "#c2413b";
  }
  if (rateLimitPercent.value < 50) {
    return "#b7791f";
  }
  return "#0f8a5f";
});
const rateLimitNote = computed(() => {
  if (rateLimit.value.reset_seconds === undefined) {
    return "no limit sample";
  }
  return `reset in ${formatRuntime(rateLimit.value.reset_seconds)}`;
});

function count(status: string): number {
  return props.state?.counts[status] ?? 0;
}
</script>
