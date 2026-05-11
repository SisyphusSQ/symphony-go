<template>
  <section class="admin-metric-card" :class="toneClass">
    <div class="admin-metric-header">
      <div>
        <div class="admin-metric-label">{{ label }}</div>
        <div class="admin-metric-value">{{ value }}</div>
      </div>
      <div v-if="$slots.icon" class="admin-metric-icon">
        <slot name="icon" />
      </div>
    </div>
    <div v-if="note" class="admin-metric-note">{{ note }}</div>
  </section>
</template>

<script setup lang="ts">
import { computed } from "vue";

const props = defineProps<{
  label: string;
  value: string | number;
  note?: string;
  tone?: "default" | "success" | "warning" | "danger" | "info";
}>();

const toneClass = computed(() => `admin-metric-${props.tone || "default"}`);
</script>
