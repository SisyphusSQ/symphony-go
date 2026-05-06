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
    </div>
    <div class="status-card">
      <a-statistic title="Retrying" :value="count('retrying')" :loading="loading" />
    </div>
    <div class="status-card">
      <a-statistic title="Failed" :value="count('failed')" :loading="loading" />
    </div>
    <div class="status-card">
      <a-statistic title="Tokens" :value="totalTokens" :loading="loading" />
    </div>
    <div class="status-card">
      <a-statistic title="Runtime" :value="runtimeText" :loading="loading" />
    </div>

    <a-alert
      v-if="fallbackReason"
      class="mock-alert"
      type="warning"
      show-icon
      :message="`Mock data active: ${fallbackReason}`"
    />
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

function count(status: string): number {
  return props.state?.counts[status] ?? 0;
}
</script>
