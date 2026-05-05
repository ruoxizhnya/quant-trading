<template>
  <div class="ai-research-page">
    <n-card title="🔬 AI 量化研究平台">
      <n-tabs v-model:value="activeTab" type="line" animated>
        <n-tab-pane name="factors" tab="因子实验室">
          <FactorLab />
        </n-tab-pane>
        <n-tab-pane name="strategies" tab="策略工坊">
          <StrategyWorkshop />
        </n-tab-pane>
        <n-tab-pane name="pipeline" tab="生成流水线">
          <PipelineDashboard />
        </n-tab-pane>
        <n-tab-pane name="evolution" tab="进化观测">
          <EvolutionObs />
        </n-tab-pane>
      </n-tabs>
    </n-card>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import FactorLab from '@/components/ai/FactorLab.vue'
import StrategyWorkshop from '@/components/ai/StrategyWorkshop.vue'
import PipelineDashboard from '@/components/ai/PipelineDashboard.vue'
import EvolutionObs from '@/components/ai/EvolutionObs.vue'

const activeTab = ref('factors')
</script>

<style scoped>
.ai-research-page {
  padding: 16px;
}
</style>
