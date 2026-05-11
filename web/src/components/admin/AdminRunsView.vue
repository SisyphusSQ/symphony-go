<template>
  <div class="admin-page">
    <div class="admin-page-heading">
      <div>
        <h1>Runs</h1>
        <p>Read-only queue and history across active, retrying, completed, and failed work.</p>
      </div>
      <a-tag>{{ runs.length }} rows</a-tag>
    </div>

    <section class="admin-panel">
      <div class="admin-query-bar">
        <a-select
          v-model:value="localStatuses"
          class="admin-query-status"
          allow-clear
          :disabled="loading"
          mode="multiple"
          :options="statusOptions"
          placeholder="Status"
          @change="submit"
        />
        <a-input-search
          v-model:value="localIssue"
          allow-clear
          class="admin-query-search"
          :disabled="loading"
          enter-button
          placeholder="Issue identifier or id"
          @search="submit"
        />
        <a-button :loading="loading" @click="submit">
          <template #icon>
            <RefreshCw :size="16" />
          </template>
          Refresh
        </a-button>
      </div>

      <a-alert v-if="error" class="admin-table-alert" show-icon type="error" :message="error" />

      <a-table
        size="middle"
        :columns="columns"
        :custom-row="customRow"
        :data-source="runs"
        :loading="loading"
        :pagination="false"
        :row-key="rowKey"
        :scroll="{ x: 960 }"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'issue'">
            <span class="admin-run-issue">{{ record.issue_identifier || record.issue_id }}</span>
          </template>
          <template v-else-if="column.key === 'status'">
            <a-tag :color="tagColor(record.status)">{{ record.status }}</a-tag>
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
            <span class="admin-latest-cell">
              {{ record.latest_event?.summary || record.error_summary || record.session_summary || "-" }}
            </span>
          </template>
        </template>
      </a-table>
    </section>
  </div>
</template>

<script setup lang="ts">
import { RefreshCw } from "lucide-vue-next";
import { ref, watch } from "vue";

import { formatDateTime, formatRuntime, formatTokens, tagColor } from "../../api/format";
import { runStatusOptions, type RunFilters, type RunRow, type RunStatus } from "../../api/operator";

const props = withDefaults(defineProps<{
  runs: RunRow[];
  filters: RunFilters;
  loading?: boolean;
  error?: string;
  selectedRunId?: string;
}>(), {
  loading: false,
  error: "",
  selectedRunId: "",
});

const emit = defineEmits<{
  submit: [filters: RunFilters];
  selectRun: [runID: string];
}>();

const localStatuses = ref<RunStatus[]>([...props.filters.statuses]);
const localIssue = ref(props.filters.issue);

const statusOptions = runStatusOptions.map((status) => ({ label: status, value: status }));
const columns = [
  { title: "Issue", key: "issue", width: 120 },
  { title: "Status", key: "status", width: 130 },
  { title: "Attempt", key: "attempt", width: 100 },
  { title: "Runtime", key: "runtime", width: 120 },
  { title: "Tokens", key: "tokens", width: 130 },
  { title: "Started", key: "started", width: 170 },
  { title: "Latest", key: "latest" },
];

function rowKey(record: RunRow): string {
  return record.run_id;
}

watch(
  () => props.filters,
  (nextFilters) => {
    localStatuses.value = [...nextFilters.statuses];
    localIssue.value = nextFilters.issue;
  },
  { deep: true },
);

function submit() {
  emit("submit", {
    statuses: [...localStatuses.value],
    issue: localIssue.value,
    limit: props.filters.limit,
  });
}

function customRow(record: RunRow) {
  return {
    class: record.run_id === props.selectedRunId ? "admin-runs-row admin-runs-row-active" : "admin-runs-row",
    onClick: () => emit("selectRun", record.run_id),
  };
}
</script>
