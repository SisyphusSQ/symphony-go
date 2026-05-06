<template>
  <section class="detail-section timeline-section" aria-label="Turn timeline">
    <div class="panel-heading timeline-heading">
      <div>
        <h2>Turn Timeline</h2>
        <p>{{ timelineSubtitle }}</p>
      </div>
      <a-select
        class="category-select"
        :options="categoryOptions"
        :value="category"
        size="small"
        @change="updateCategory"
      />
    </div>

    <a-spin :spinning="loading">
      <a-alert v-if="error" type="error" show-icon :message="error" />
      <a-empty v-else-if="events.length === 0" description="No timeline events" />
      <a-timeline v-else class="operation-timeline">
        <a-timeline-item
          v-for="event in events"
          :key="event.id"
          :color="timelineColor(event.severity)"
        >
          <button
            type="button"
            class="timeline-event"
            :class="{ 'timeline-event-selected': event.id === selectedEventID }"
            @click="$emit('select', event.id)"
          >
            <span class="timeline-event-head">
              <span class="timeline-title">{{ event.title }}</span>
              <span class="timeline-meta">
                <a-tag :color="categoryColor(event.category)">{{ event.category }}</a-tag>
                <a-tag :color="severityColor(event.severity)">{{ event.severity }}</a-tag>
                <span class="timeline-time">{{ formatDateTime(event.at) }}</span>
              </span>
            </span>
            <span class="timeline-summary">{{ event.summary }}</span>
            <span class="timeline-foot">
              <span class="mono">#{{ event.sequence }}</span>
              <span v-if="event.session_id" class="mono">{{ event.session_id }}</span>
              <span v-if="event.turn_id" class="mono">{{ event.turn_id }}</span>
              <span v-if="durationText(event)">{{ durationText(event) }}</span>
              <span v-if="tokenText(event)">{{ tokenText(event) }} tokens</span>
            </span>
          </button>
        </a-timeline-item>
      </a-timeline>
    </a-spin>
  </section>
</template>

<script setup lang="ts">
import { computed } from "vue";

import {
  eventDurationMS,
  formatDateTime,
  formatDurationMS,
  formatEventTokens,
  statusTone,
} from "../api/format";
import {
  timelineCategories,
  type RunTimelineEvent,
  type TimelineCategoryFilter,
} from "../api/operator";

const props = defineProps<{
  events: RunTimelineEvent[];
  loading: boolean;
  error: string;
  category: TimelineCategoryFilter;
  selectedEventID: string;
}>();

const emit = defineEmits<{
  category: [category: TimelineCategoryFilter];
  select: [eventID: string];
}>();

const categoryOptions = [
  { label: "All", value: "all" },
  ...timelineCategories.map((category) => ({
    label: category,
    value: category,
  })),
];

const timelineSubtitle = computed(() => {
  if (props.loading) {
    return "Loading events";
  }
  return `${props.events.length} events`;
});

function updateCategory(value: string | number) {
  emit("category", String(value) as TimelineCategoryFilter);
}

function timelineColor(severity: string): string {
  switch (severity) {
    case "error":
      return "red";
    case "warning":
    case "warn":
      return "orange";
    case "debug":
      return "gray";
    default:
      return "blue";
  }
}

function categoryColor(category: string): string {
  switch (category) {
    case "tool":
      return "geekblue";
    case "command":
      return "cyan";
    case "diff":
      return "purple";
    case "resource":
      return "gold";
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

function severityColor(severity: string): string {
  const tone = statusTone(severity);
  return tone === "error" ? "red" : tone === "warning" ? "gold" : "default";
}

function durationText(event: RunTimelineEvent): string {
  return formatDurationMS(eventDurationMS(event));
}

function tokenText(event: RunTimelineEvent): string {
  return formatEventTokens(event);
}
</script>
