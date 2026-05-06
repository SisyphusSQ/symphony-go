<template>
  <main class="operator-shell">
    <header class="shell-header">
      <div>
        <h1>Symphony Operator</h1>
        <p>{{ headerSubtitle }}</p>
      </div>
      <a-badge :status="source === 'api' ? 'processing' : 'warning'" :text="sourceLabel" />
    </header>

    <StatusBar
      :fallback-reason="fallbackReason"
      :loading="loading"
      :source="source"
      :state="state"
    />

    <section class="dashboard-grid">
      <div class="runs-panel">
        <RunFilters :filters="filters" :loading="loading" @submit="updateFilters" />
        <RunList
          :error="error"
          :loading="loading"
          :runs="runs"
          :selected-run-i-d="selectedRunID"
          @select="selectRun"
        />
      </div>

      <aside class="detail-panel" aria-label="Selected run summary">
        <div class="panel-heading">
          <h2>Run Summary</h2>
          <a-tag v-if="selectedDetail" :color="tagColor(selectedDetail.metadata.status)">
            {{ selectedDetail.metadata.status }}
          </a-tag>
        </div>

        <a-spin :spinning="detailLoading">
          <a-alert v-if="detailError" type="error" show-icon :message="detailError" />
          <a-empty v-else-if="!selectedDetail" description="Select a run" />
          <a-descriptions
            v-else
            size="small"
            :column="1"
            bordered
            class="detail-descriptions"
          >
            <a-descriptions-item label="Issue">
              {{ selectedDetail.issue.identifier || selectedDetail.issue.id }}
            </a-descriptions-item>
            <a-descriptions-item label="Run ID">
              <span class="mono">{{ selectedDetail.metadata.run_id }}</span>
            </a-descriptions-item>
            <a-descriptions-item label="Attempt">
              #{{ selectedDetail.metadata.attempt }}
            </a-descriptions-item>
            <a-descriptions-item label="Runtime">
              {{ formatRuntime(selectedDetail.metadata.runtime_seconds) }}
            </a-descriptions-item>
            <a-descriptions-item label="Session">
              {{ selectedDetail.session.id || "-" }}
            </a-descriptions-item>
            <a-descriptions-item label="Workspace">
              <span class="workspace-path">{{ selectedDetail.workspace.path || "-" }}</span>
            </a-descriptions-item>
            <a-descriptions-item label="Latest">
              {{ selectedDetail.latest_event?.summary || selectedDetail.session.summary || "-" }}
            </a-descriptions-item>
            <a-descriptions-item v-if="selectedDetail.failure" label="Failure">
              <span class="failure-text">{{ selectedDetail.failure.error }}</span>
            </a-descriptions-item>
          </a-descriptions>
        </a-spin>
      </aside>
    </section>
  </main>
</template>

<script setup lang="ts">
import { computed, onMounted } from "vue";

import { formatRuntime, tagColor } from "../api/format";
import type { OperatorApiClient } from "../api/operator";
import RunFilters from "../components/RunFilters.vue";
import RunList from "../components/RunList.vue";
import StatusBar from "../components/StatusBar.vue";
import { useDashboardData } from "../composables/useDashboardData";

const props = defineProps<{
  client?: OperatorApiClient;
}>();

const {
  state,
  runs,
  selectedDetail,
  selectedRunID,
  loading,
  detailLoading,
  error,
  detailError,
  source,
  fallbackReason,
  filters,
  loadDashboard,
  selectRun,
  updateFilters,
} = useDashboardData(props.client);

const sourceLabel = computed(() => (source.value === "api" ? "API live" : "Mock data"));
const headerSubtitle = computed(() => {
  const generatedAt = state.value?.generated_at;
  return generatedAt ? `Generated ${new Date(generatedAt).toLocaleString()}` : "Waiting for operator state";
});

onMounted(() => {
  void loadDashboard();
});
</script>
