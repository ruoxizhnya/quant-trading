<script setup lang="ts">
import { NH1 } from 'naive-ui'
import FactorCitation from '@/components/factor/FactorCitation.vue'
</script>

<template>
  <div class="factor-citation-page">
    <NH1>因子证据</NH1>
    <FactorCitation />
  </div>
</template>

<style scoped>
.factor-citation-page {
  padding: 24px;
  max-width: 1200px;
  margin: 0 auto;
}
</style>