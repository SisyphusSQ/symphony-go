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

      <div class="detail-panel" aria-label="Selected run detail">
        <RunDetailSummary
          :detail="selectedDetail"
          :error="detailError"
          :loading="detailLoading"
        />
        <RunTimeline
          :category="eventFilters.category"
          :error="eventsError"
          :events="timelineEvents"
          :loading="eventsLoading"
          :selected-event-i-d="selectedEventID"
          @category="updateEventCategory"
          @select="selectEvent"
        />
      </div>

      <TimelineEventDetail :event="selectedEvent" />
    </section>
  </main>
</template>

<script setup lang="ts">
import { computed, onMounted } from "vue";

import type { OperatorApiClient } from "../api/operator";
import RunFilters from "../components/RunFilters.vue";
import RunList from "../components/RunList.vue";
import RunDetailSummary from "../components/RunDetailSummary.vue";
import RunTimeline from "../components/RunTimeline.vue";
import StatusBar from "../components/StatusBar.vue";
import TimelineEventDetail from "../components/TimelineEventDetail.vue";
import { useDashboardData } from "../composables/useDashboardData";

const props = defineProps<{
  client?: OperatorApiClient;
}>();

const {
  state,
  runs,
  selectedDetail,
  timelineEvents,
  selectedEvent,
  selectedEventID,
  selectedRunID,
  loading,
  detailLoading,
  eventsLoading,
  error,
  detailError,
  eventsError,
  source,
  fallbackReason,
  filters,
  eventFilters,
  loadDashboard,
  selectRun,
  selectEvent,
  updateFilters,
  updateEventCategory,
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
