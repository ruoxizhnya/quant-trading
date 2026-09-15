<script setup lang="ts">
import { NH1 } from 'naive-ui'
import EvidenceLookup from '@/components/evidence/EvidenceLookup.vue'
</script>

<template>
  <div class="evidence-page">
    <NH1>证据查询</NH1>
    <EvidenceLookup />
  </div>
</template>

<style scoped>
.evidence-page {
  padding: 24px;
  max-width: 1200px;
  margin: 0 auto;
}
</style>