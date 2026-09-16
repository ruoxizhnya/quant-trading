<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { NH1 } from 'naive-ui'
import EvidenceLookup from '@/components/evidence/EvidenceLookup.vue'

// TASKS.md P5-3: the citation tuple's "回溯证据" action lands here with the
// hash already in the query string, so the page resolves it without typing.
const route = useRoute()
const contentHash = computed(() =>
  typeof route.query.content_hash === 'string' ? route.query.content_hash : '',
)
</script>

<template>
  <div class="evidence-page">
    <NH1>证据查询</NH1>
    <EvidenceLookup :initial-hash="contentHash" />
  </div>
</template>

<style scoped>
.evidence-page {
  padding: 24px;
  max-width: 1200px;
  margin: 0 auto;
}
</style>