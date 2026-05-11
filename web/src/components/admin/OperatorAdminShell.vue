<template>
  <div class="admin-shell">
    <aside class="admin-sidebar" aria-label="Operator navigation">
      <div class="admin-brand">
        <div class="admin-brand-mark">S</div>
        <div>
          <div class="admin-brand-name">Symphony</div>
          <div class="admin-brand-subtitle">Operator Console</div>
        </div>
      </div>

      <nav class="admin-nav">
        <button
          v-for="item in navItems"
          :key="item.key"
          class="admin-nav-item"
          :class="{ active: item.key === activeView }"
          type="button"
          @click="$emit('navigate', item.key)"
        >
          <component :is="item.icon" :size="18" />
          <span>{{ item.label }}</span>
        </button>
      </nav>
    </aside>

    <section class="admin-main">
      <header class="admin-topbar">
        <div>
          <div class="admin-topbar-kicker">Local Operator</div>
          <div class="admin-topbar-title">{{ title }}</div>
        </div>
        <div class="admin-topbar-actions">
          <a-tag :color="source === 'api' ? 'blue' : 'gold'">
            {{ source === "api" ? "API live" : "Mock data" }}
          </a-tag>
          <a-tag :color="ready ? 'green' : 'gold'">
            {{ ready ? "Ready" : "Not ready" }}
          </a-tag>
        </div>
      </header>

      <main class="admin-content">
        <slot />
      </main>
    </section>
  </div>
</template>

<script setup lang="ts">
import { Gauge, LayoutDashboard } from "lucide-vue-next";

import type { OperatorDataSource } from "../../api/operator";

defineProps<{
  activeView: "dashboard" | "runs";
  title: string;
  source: OperatorDataSource;
  ready: boolean;
}>();

defineEmits<{
  navigate: [view: "dashboard" | "runs"];
}>();

const navItems = [
  {
    key: "dashboard" as const,
    label: "Dashboard",
    icon: LayoutDashboard,
  },
  {
    key: "runs" as const,
    label: "Runs",
    icon: Gauge,
  },
];
</script>
