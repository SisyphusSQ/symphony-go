<template>
  <div class="admin-page">
    <div class="admin-page-heading">
      <div>
        <h1>Operator Overview</h1>
        <p>{{ generatedText }}</p>
      </div>
      <a-badge :status="state.ready.ok ? 'success' : 'warning'" :text="state.ready.ok ? 'Dispatch ready' : state.ready.error || 'Not ready'" />
    </div>

    <section class="admin-metric-grid">
      <AdminMetricCard label="Running" :value="count('running')" note="active sessions" tone="info" />
      <AdminMetricCard label="Retrying" :value="count('retrying')" note="backoff queue" tone="warning" />
      <AdminMetricCard label="Failed" :value="count('failed')" note="needs review" tone="danger" />
      <AdminMetricCard label="Tokens" :value="formatCount(state.tokens.total_tokens)" note="aggregate total" />
      <AdminMetricCard label="Runtime" :value="formatRuntime(state.runtime.total_seconds)" note="all visible runs" />
      <AdminMetricCard label="Rate limit" :value="rateLimitText" :note="rateLimitNote" tone="success" />
    </section>

    <section class="admin-work-grid">
      <section class="admin-panel">
        <div class="admin-panel-header">
          <div>
            <h2>Recent Runs</h2>
            <p>Running, retrying, completed, and failed work</p>
          </div>
          <a-tag>{{ runs.length }} rows</a-tag>
        </div>
        <div class="admin-run-summary-list">
          <button
            v-for="run in runs"
            :key="run.run_id"
            class="admin-run-summary"
            :class="{ active: selectedDetail?.metadata.run_id === run.run_id }"
            type="button"
            @click="$emit('selectRun', run.run_id)"
          >
            <span class="admin-run-issue">{{ run.issue_identifier || run.issue_id }}</span>
            <a-tag :color="tagColor(run.status)">{{ run.status }}</a-tag>
            <span>{{ formatRuntime(run.runtime_seconds) }}</span>
            <span>{{ run.latest_event?.summary || run.error_summary || run.session_summary || "-" }}</span>
          </button>
          <a-empty v-if="runs.length === 0" description="No runs match the current filters" />
        </div>
      </section>

      <section class="admin-panel">
        <div class="admin-panel-header">
          <div>
            <h2>Selected Run</h2>
            <p>Current detail payload</p>
          </div>
        </div>
        <AdminRunDetailView v-if="selectedDetail" :detail="selectedDetail" compact :show-events="false" />
        <a-empty v-else description="Select a run" />
      </section>
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed } from "vue";

import { formatCount, formatRuntime, tagColor } from "../../api/format";
import type { RunDetail, RunRow, StateResponse } from "../../api/operator";
import AdminMetricCard from "./AdminMetricCard.vue";
import AdminRunDetailView from "./AdminRunDetailView.vue";

const props = defineProps<{
  state: StateResponse;
  runs: RunRow[];
  selectedDetail: RunDetail | null;
}>();

defineEmits<{
  selectRun: [runID: string];
}>();

const generatedText = computed(() => `Generated ${new Date(props.state.generated_at).toLocaleString()}`);
const rateLimit = computed(() => {
  const latest = props.state.rate_limit.latest;
  if (!latest || typeof latest !== "object") {
    return {};
  }
  return latest as { primary_remaining?: number; reset_seconds?: number };
});
const rateLimitText = computed(() =>
  rateLimit.value.primary_remaining === undefined ? "-" : formatCount(rateLimit.value.primary_remaining),
);
const rateLimitNote = computed(() =>
  rateLimit.value.reset_seconds === undefined
    ? "no sample"
    : `reset in ${formatRuntime(rateLimit.value.reset_seconds)}`,
);

function count(status: string): number {
  return props.state.counts[status] ?? 0;
}
</script>
