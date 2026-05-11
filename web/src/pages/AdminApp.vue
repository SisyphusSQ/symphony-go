<template>
  <OperatorAdminShell
    :active-view="shellActiveView"
    :ready="ready"
    :source="source"
    :title="title"
    @navigate="navigate"
  >
    <a-spin :spinning="loading && !state">
      <section v-if="error && !state" class="admin-state-panel">
        <a-alert show-icon type="error" :message="error" />
      </section>

      <AdminDashboardView
        v-else-if="state && activeView === 'dashboard'"
        :runs="runs"
        :selected-detail="selectedDetail"
        :state="state"
        @select-run="openRunDetail"
      />

      <AdminRunsView
        v-else-if="activeView === 'runs'"
        :error="error"
        :filters="filters"
        :loading="loading"
        :runs="runs"
        :selected-run-id="selectedRunID"
        @select-run="openRunDetail"
        @submit="handleFilterSubmit"
      />

      <section v-else-if="activeView === 'run-detail'" class="admin-page">
        <a-spin :spinning="detailLoading">
          <a-alert v-if="detailError" show-icon type="error" :message="detailError" />
          <AdminRunDetailView
            v-else-if="selectedDetail"
            :detail="selectedDetail"
            :events="selectedEvents"
            :events-error="eventsError"
            :events-loading="eventsLoading"
          />
          <section v-else class="admin-state-panel">
            <a-empty description="Select a run" />
          </section>
        </a-spin>
      </section>
    </a-spin>
  </OperatorAdminShell>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from "vue";

import type { OperatorApiClient, RunFilters } from "../api/operator";
import AdminDashboardView from "../components/admin/AdminDashboardView.vue";
import AdminRunDetailView from "../components/admin/AdminRunDetailView.vue";
import AdminRunsView from "../components/admin/AdminRunsView.vue";
import OperatorAdminShell from "../components/admin/OperatorAdminShell.vue";
import { useDashboardData } from "../composables/useDashboardData";

type AdminView = "dashboard" | "runs" | "run-detail";
type ShellView = "dashboard" | "runs";

const props = defineProps<{
  client?: OperatorApiClient;
}>();

const activeView = ref<AdminView>("dashboard");

const {
  state,
  runs,
  selectedDetail,
  selectedEvents,
  selectedRunID,
  loading,
  detailLoading,
  eventsLoading,
  error,
  detailError,
  eventsError,
  source,
  filters,
  loadDashboard,
  selectRun,
  updateFilters,
} = useDashboardData(props.client);

const shellActiveView = computed<ShellView>(() => (activeView.value === "dashboard" ? "dashboard" : "runs"));
const ready = computed(() => state.value?.ready.ok ?? false);
const title = computed(() => {
  if (activeView.value === "dashboard") {
    return "Operator Overview";
  }
  if (activeView.value === "run-detail") {
    return "Run Detail";
  }
  return "Runs";
});

function navigate(view: ShellView) {
  activeView.value = view;
}

function openRunDetail(runID: string) {
  activeView.value = "run-detail";
  void selectRun(runID);
}

function handleFilterSubmit(nextFilters: RunFilters) {
  activeView.value = "runs";
  void updateFilters(nextFilters);
}

onMounted(() => {
  void loadDashboard();
});
</script>
