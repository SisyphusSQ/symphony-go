<template>
  <div class="admin-detail" :class="{ compact }">
    <div v-if="!compact" class="admin-page-heading">
      <div>
        <h1>{{ detail.issue.identifier || detail.issue.id }}</h1>
        <p>Run detail from the existing operator detail payload.</p>
      </div>
      <a-tag :color="tagColor(detail.metadata.status)">{{ detail.metadata.status }}</a-tag>
    </div>

    <section class="admin-detail-metrics">
      <AdminMetricCard label="Attempt" :value="`#${detail.metadata.attempt}`" />
      <AdminMetricCard label="Runtime" :value="formatRuntime(detail.metadata.runtime_seconds)" />
      <AdminMetricCard label="Tokens" :value="formatTokens(detail.token_totals)" />
    </section>

    <a-alert
      v-if="detail.failure"
      class="admin-detail-alert"
      type="error"
      show-icon
      :message="detail.failure.error"
    />
    <a-alert
      v-else-if="detail.retry"
      class="admin-detail-alert"
      type="warning"
      show-icon
      :message="detail.retry.error || 'Retry scheduled'"
    />

    <section class="admin-panel admin-detail-panel">
      <div class="admin-panel-header">
        <div>
          <h2>Latest Signal</h2>
          <p>{{ detail.latest_event?.at || detail.metadata.started_at }}</p>
        </div>
      </div>
      <div class="admin-latest-signal">
        {{ detail.latest_event?.summary || detail.session.summary || "-" }}
      </div>
    </section>

    <section v-if="shouldShowEvents" class="admin-panel admin-detail-panel">
      <div class="admin-panel-header">
        <div>
          <h2>Event Timeline</h2>
          <p>Redacted projection from /api/v1/runs/{run_id}/events</p>
        </div>
        <a-tag>{{ events.length }} events</a-tag>
      </div>
      <a-spin :spinning="eventsLoading">
        <a-alert
          v-if="eventsError"
          class="admin-timeline-alert"
          show-icon
          type="warning"
          :message="eventsError"
        />
        <div v-else-if="events.length > 0" class="admin-timeline-wrap">
          <a-timeline>
            <a-timeline-item
              v-for="event in events"
              :key="event.id"
              :color="timelineColor(event.severity)"
            >
              <article class="admin-timeline-event">
                <div class="admin-timeline-event-head">
                  <div>
                    <div class="admin-timeline-title">{{ event.title }}</div>
                    <div class="admin-timeline-time">{{ formatDateTime(event.at) }}</div>
                  </div>
                  <div class="admin-timeline-tags">
                    <a-tag :color="categoryColor(event.category)">{{ event.category }}</a-tag>
                    <a-tag :color="severityColor(event.severity)">{{ event.severity }}</a-tag>
                  </div>
                </div>
                <p>{{ event.summary }}</p>
                <a-collapse v-if="event.payload" ghost size="small" class="admin-payload-collapse">
                  <a-collapse-panel key="payload" header="Redacted payload">
                    <pre class="admin-payload-json">{{ formatPayload(event.payload) }}</pre>
                  </a-collapse-panel>
                </a-collapse>
              </article>
            </a-timeline-item>
          </a-timeline>
        </div>
        <a-empty v-else description="No events recorded for this run" />
      </a-spin>
    </section>

    <section class="admin-panel admin-detail-panel">
      <div class="admin-panel-header">
        <div>
          <h2>Metadata</h2>
          <p>Session, run, and workspace identifiers</p>
        </div>
      </div>
      <a-descriptions size="small" :column="1" bordered>
        <a-descriptions-item label="Run ID">
          <span class="mono">{{ detail.metadata.run_id }}</span>
        </a-descriptions-item>
        <a-descriptions-item label="Session">
          <span class="mono">{{ detail.session.id || "-" }}</span>
        </a-descriptions-item>
        <a-descriptions-item label="Thread">
          <span class="mono">{{ detail.session.thread_id || "-" }}</span>
        </a-descriptions-item>
        <a-descriptions-item label="Workspace">
          <span class="workspace-path">{{ detail.workspace.path || "-" }}</span>
        </a-descriptions-item>
      </a-descriptions>
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed } from "vue";

import { formatDateTime, formatRuntime, formatTokens, tagColor } from "../../api/format";
import type { RunDetail, RunTimelineEvent } from "../../api/operator";
import AdminMetricCard from "./AdminMetricCard.vue";

const props = withDefaults(defineProps<{
  detail: RunDetail;
  events?: RunTimelineEvent[];
  eventsLoading?: boolean;
  eventsError?: string;
  compact?: boolean;
  showEvents?: boolean;
}>(), {
  events: () => [],
  eventsLoading: false,
  eventsError: "",
  showEvents: true,
});

const shouldShowEvents = computed(() => props.showEvents);

function timelineColor(severity: string): string {
  if (severity === "error") {
    return "red";
  }
  if (severity === "warn" || severity === "warning") {
    return "orange";
  }
  if (severity === "debug") {
    return "gray";
  }
  return "blue";
}

function severityColor(severity: string): string {
  if (severity === "error") {
    return "red";
  }
  if (severity === "warn" || severity === "warning") {
    return "gold";
  }
  if (severity === "debug") {
    return "default";
  }
  return "blue";
}

function categoryColor(category: string): string {
  switch (category) {
    case "tool":
      return "purple";
    case "command":
      return "geekblue";
    case "resource":
      return "cyan";
    case "guardrail":
      return "volcano";
    case "error":
      return "red";
    case "message":
      return "green";
    default:
      return "blue";
  }
}

function formatPayload(payload: unknown): string {
  if (payload === undefined || payload === null) {
    return "{}";
  }
  if (typeof payload === "string") {
    return payload;
  }
  return JSON.stringify(payload, null, 2);
}
</script>
