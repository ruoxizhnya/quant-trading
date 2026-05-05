<script setup lang="ts">
import { NSpace, NGrid, NGridItem, NH1 } from 'naive-ui'
import DataSourcePanel from '@/components/sync/DataSourcePanel.vue'
import DataSourceSwitch from '@/components/sync/DataSourceSwitch.vue'
import SyncStatusPanel from '@/components/sync/SyncStatusPanel.vue'
import DataImportForm from '@/components/sync/DataImportForm.vue'
</script>

<template>
  <div class="data-sync-page">
    <NH1>数据同步中心</NH1>

    <NGrid :cols="2" :x-gap="16" :y-gap="16" responsive="screen">
      <NGridItem>
        <DataSourcePanel />
      </NGridItem>

      <NGridItem>
        <DataSourceSwitch />
      </NGridItem>

      <NGridItem>
        <SyncStatusPanel />
      </NGridItem>

      <NGridItem>
        <DataImportForm />
      </NGridItem>
    </NGrid>
  </div>
</template>

<style scoped>
.data-sync-page {
  padding: 24px;
  max-width: 1200px;
  margin: 0 auto;
}
</style>
