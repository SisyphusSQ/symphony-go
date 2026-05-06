<template>
  <section class="detail-section" aria-label="Run metadata">
    <div class="panel-heading">
      <h2>Run Detail</h2>
      <a-tag v-if="detail" :color="tagColor(detail.metadata.status)">
        {{ detail.metadata.status }}
      </a-tag>
    </div>

    <a-spin :spinning="loading">
      <a-alert v-if="error" type="error" show-icon :message="error" />
      <a-empty v-else-if="!detail" description="Select a run" />
      <a-descriptions v-else size="small" :column="2" bordered class="detail-descriptions">
        <a-descriptions-item label="Issue">
          {{ detail.issue.identifier || detail.issue.id }}
        </a-descriptions-item>
        <a-descriptions-item label="Attempt">
          #{{ detail.metadata.attempt }}
        </a-descriptions-item>
        <a-descriptions-item label="Run ID">
          <span class="mono">{{ detail.metadata.run_id }}</span>
        </a-descriptions-item>
        <a-descriptions-item label="Runtime">
          {{ formatRuntime(detail.metadata.runtime_seconds) }}
        </a-descriptions-item>
        <a-descriptions-item label="Started">
          {{ formatDateTime(detail.metadata.started_at) }}
        </a-descriptions-item>
        <a-descriptions-item label="Finished">
          {{ formatDateTime(detail.metadata.finished_at) }}
        </a-descriptions-item>
        <a-descriptions-item label="Session">
          <span class="mono">{{ detail.session.id || "-" }}</span>
        </a-descriptions-item>
        <a-descriptions-item label="Turn">
          <span class="mono">{{ detail.session.turn_id || detail.session.thread_id || "-" }}</span>
        </a-descriptions-item>
        <a-descriptions-item label="Tokens">
          {{ formatTokens(detail.token_totals) }}
        </a-descriptions-item>
        <a-descriptions-item label="Workspace">
          <span class="workspace-path">{{ detail.workspace.path || "-" }}</span>
        </a-descriptions-item>
        <a-descriptions-item label="Latest">
          {{ detail.latest_event?.summary || detail.session.summary || "-" }}
        </a-descriptions-item>
        <a-descriptions-item v-if="detail.retry" label="Retry">
          #{{ detail.retry.attempt }} · {{ formatDateTime(detail.retry.due_at) }}
        </a-descriptions-item>
        <a-descriptions-item v-if="detail.failure" label="Failure">
          <span class="failure-text">{{ detail.failure.error }}</span>
        </a-descriptions-item>
      </a-descriptions>
    </a-spin>
  </section>
</template>

<script setup lang="ts">
import { formatDateTime, formatRuntime, formatTokens, tagColor } from "../api/format";
import type { RunDetail } from "../api/operator";

defineProps<{
  detail: RunDetail | null;
  loading: boolean;
  error: string;
}>();
</script>
