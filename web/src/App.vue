<template>
  <a-config-provider :theme="theme">
    <AdminApp />
  </a-config-provider>
</template>

<script setup lang="ts">
import AdminApp from "./pages/AdminApp.vue";
import { operatorTheme } from "./theme";

const theme = operatorTheme;
</script>
