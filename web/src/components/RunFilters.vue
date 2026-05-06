<template>
  <section class="run-filters" aria-label="Run filters">
    <a-select
      v-model:value="localStatuses"
      class="status-filter"
      mode="multiple"
      allow-clear
      :disabled="loading"
      :options="statusSelectOptions"
      placeholder="Status"
      @change="submit"
    />
    <a-input-search
      v-model:value="localIssue"
      class="issue-filter"
      allow-clear
      enter-button
      :disabled="loading"
      placeholder="Issue identifier or id"
      @search="submit"
    />
    <a-button class="refresh-button" :loading="loading" @click="submit">
      <template #icon>
        <RefreshCw :size="16" />
      </template>
      Refresh
    </a-button>
  </section>
</template>

<script setup lang="ts">
import { RefreshCw } from "lucide-vue-next";
import { ref, watch } from "vue";

import { runStatusOptions, type RunFilters, type RunStatus } from "../api/operator";

const props = defineProps<{
  filters: RunFilters;
  loading: boolean;
}>();

const emit = defineEmits<{
  submit: [filters: RunFilters];
}>();

const localStatuses = ref<RunStatus[]>([...props.filters.statuses]);
const localIssue = ref(props.filters.issue);

const statusSelectOptions = runStatusOptions.map((status) => ({
  value: status,
  label: status,
}));

watch(
  () => props.filters,
  (filters) => {
    localStatuses.value = [...filters.statuses];
    localIssue.value = filters.issue;
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
</script>
