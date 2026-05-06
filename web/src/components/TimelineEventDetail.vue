<template>
  <aside class="event-panel" aria-label="Timeline event detail">
    <div class="panel-heading">
      <h2>Event Detail</h2>
      <a-tag v-if="event" :color="event.severity === 'error' ? 'red' : 'default'">
        {{ event.severity }}
      </a-tag>
    </div>

    <a-empty v-if="!event" description="Select an event" />
    <template v-else>
      <a-descriptions size="small" :column="1" bordered class="event-descriptions">
        <a-descriptions-item label="Title">
          {{ event.title }}
        </a-descriptions-item>
        <a-descriptions-item label="Summary">
          {{ event.summary }}
        </a-descriptions-item>
        <a-descriptions-item label="Timestamp">
          {{ formatDateTime(event.at) }}
        </a-descriptions-item>
        <a-descriptions-item label="Category">
          {{ event.category }}
        </a-descriptions-item>
        <a-descriptions-item label="Duration">
          {{ durationText || "-" }}
        </a-descriptions-item>
        <a-descriptions-item label="Tokens">
          {{ tokenText || "-" }}
        </a-descriptions-item>
        <a-descriptions-item label="Run">
          <span class="mono">{{ event.run_id }}</span>
        </a-descriptions-item>
        <a-descriptions-item label="Context">
          <span class="mono">{{ contextText }}</span>
        </a-descriptions-item>
      </a-descriptions>

      <a-collapse class="payload-collapse" :bordered="false">
        <a-collapse-panel key="payload" header="Raw redacted JSON">
          <div class="payload-toolbar">
            <a-button size="small" type="default" @click="copyPayload">
              <template #icon>
                <Copy :size="14" />
              </template>
              Copy payload
            </a-button>
            <span class="copy-status">{{ copyStatus }}</span>
          </div>
          <pre class="payload-json">{{ payloadJSON }}</pre>
        </a-collapse-panel>
      </a-collapse>
    </template>
  </aside>
</template>

<script setup lang="ts">
import { Copy } from "lucide-vue-next";
import { computed, ref, watch } from "vue";

import {
  eventDurationMS,
  formatDateTime,
  formatDurationMS,
  formatEventTokens,
} from "../api/format";
import type { RunTimelineEvent } from "../api/operator";

const props = defineProps<{
  event: RunTimelineEvent | null;
}>();

const copyStatus = ref("");

const payloadJSON = computed(() => {
  if (!props.event) {
    return "";
  }
  return JSON.stringify(props.event.payload, null, 2);
});

const durationText = computed(() => (props.event ? formatDurationMS(eventDurationMS(props.event)) : ""));
const tokenText = computed(() => (props.event ? formatEventTokens(props.event) : ""));
const contextText = computed(() => {
  if (!props.event) {
    return "-";
  }
  return [props.event.session_id, props.event.thread_id, props.event.turn_id]
    .filter(Boolean)
    .join(" / ") || "-";
});

watch(
  () => props.event?.id,
  () => {
    copyStatus.value = "";
  },
);

async function copyPayload() {
  if (!payloadJSON.value) {
    return;
  }
  try {
    await writeClipboard(payloadJSON.value);
    copyStatus.value = "Copied";
  } catch (error) {
    copyStatus.value = error instanceof Error ? error.message : "Copy failed";
  }
}

async function writeClipboard(text: string) {
  if (navigator.clipboard?.writeText) {
    await navigator.clipboard.writeText(text);
    return;
  }
  const element = document.createElement("textarea");
  element.value = text;
  element.setAttribute("readonly", "true");
  element.style.position = "fixed";
  element.style.left = "-9999px";
  document.body.appendChild(element);
  element.select();
  const copied = document.execCommand("copy");
  document.body.removeChild(element);
  if (!copied) {
    throw new Error("Copy failed");
  }
}
</script>
