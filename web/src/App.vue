<template>
  <a-config-provider :theme="theme">
    <Dashboard />
  </a-config-provider>
</template>

<script setup lang="ts">
import type { ThemeConfig } from "ant-design-vue/es/config-provider/context";

import Dashboard from "./pages/Dashboard.vue";

const theme: ThemeConfig = {
  token: {
    borderRadius: 6,
    colorPrimary: "#1677ff",
    colorSuccess: "#168a4a",
    colorWarning: "#b7791f",
    colorError: "#c2413b",
    fontFamily:
      'Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif',
  },
};
</script>
