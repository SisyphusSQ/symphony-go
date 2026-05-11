<template>
  <section class="run-list" aria-label="Run list">
    <a-alert
      v-if="error"
      class="table-alert"
      type="error"
      show-icon
      :message="error"
    />
    <a-table
      data-testid="run-list-table"
      size="small"
      :columns="columns"
      :custom-row="customRow"
      :data-source="runs"
      :loading="loading"
      :locale="tableLocale"
      :pagination="false"
      :row-class-name="rowClassName"
      :row-key="rowKey"
      :scroll="{ x: 920 }"
    >
      <template #bodyCell="{ column, record }">
        <template v-if="column.key === 'issue'">
          <span class="issue-cell">{{ record.issue_identifier || record.issue_id }}</span>
        </template>
        <template v-else-if="column.key === 'status'">
          <span class="status-cell">
            <a-badge :status="statusTone(record.status)" />
            <a-tag :color="tagColor(record.status)">{{ record.status }}</a-tag>
          </span>
        </template>
        <template v-else-if="column.key === 'attempt'">
          #{{ record.attempt }}
        </template>
        <template v-else-if="column.key === 'runtime'">
          {{ formatRuntime(record.runtime_seconds) }}
        </template>
        <template v-else-if="column.key === 'tokens'">
          {{ formatTokens(record.token_totals) }}
        </template>
        <template v-else-if="column.key === 'started'">
          {{ formatDateTime(record.started_at) }}
        </template>
        <template v-else-if="column.key === 'latest'">
          <span class="latest-cell">{{ record.latest_event?.summary || record.error_summary || record.session_summary || "-" }}</span>
        </template>
      </template>
    </a-table>
  </section>
</template>

<script setup lang="ts">
import { computed } from "vue";

import { formatDateTime, formatRuntime, formatTokens, statusTone, tagColor } from "../api/format";
import type { RunRow } from "../api/operator";

const props = defineProps<{
  runs: RunRow[];
  loading: boolean;
  error: string;
  selectedRunID: string;
}>();

const emit = defineEmits<{
  select: [runID: string];
}>();

const columns = [
  { title: "Issue", key: "issue", width: 110 },
  { title: "Status", key: "status", width: 120 },
  { title: "Attempt", key: "attempt", width: 90 },
  { title: "Runtime", key: "runtime", width: 110 },
  { title: "Tokens", key: "tokens", width: 120 },
  { title: "Started", key: "started", width: 150 },
  { title: "Latest", key: "latest" },
];

const tableLocale = computed(() => ({
  emptyText: props.error ? "No rows available" : "No runs match the current filters",
}));

function rowKey(record: RunRow): string {
  return record.run_id;
}

function customRow(record: RunRow) {
  return {
    onClick: () => emit("select", record.run_id),
    onKeydown: (event: KeyboardEvent) => {
      if (event.key === "Enter" || event.key === " ") {
        event.preventDefault();
        emit("select", record.run_id);
      }
    },
    role: "button",
    tabindex: 0,
  };
}

function rowClassName(record: RunRow): string {
  return record.run_id === props.selectedRunID ? "selected-run-row" : "";
}
</script>
