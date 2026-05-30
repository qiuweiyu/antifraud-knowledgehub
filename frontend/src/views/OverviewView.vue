<template>
  <section class="grid">
    <div class="grid cols-4">
      <div class="metric"><strong>{{ categories.length }}</strong><span>诈骗分类</span></div>
      <div class="metric"><strong>{{ rules.length }}</strong><span>风险规则</span></div>
      <div class="metric"><strong>{{ cases.length }}</strong><span>匿名案例</span></div>
      <div class="metric"><strong>{{ recentCount }}</strong><span>最近分析次数</span></div>
    </div>
    <div class="grid cols-2">
      <div class="panel">
        <h2>项目概览</h2>
        <p class="muted">结构化知识库、可解释规则引擎、REST API、CLI 与 Vue3 控制台，适合公共安全教育和开发者集成。</p>
        <div class="grid cols-2">
          <el-alert title="Explainable risk analysis" type="success" :closable="false" />
          <el-alert title="Chinese-speaking scam scenarios" type="warning" :closable="false" />
          <el-alert title="Community-maintained rules" type="info" :closable="false" />
          <el-alert title="Developer-friendly API" type="error" :closable="false" />
        </div>
      </div>
      <div class="panel">
        <h2>风险分类分布</h2>
        <div ref="chartRef" style="height: 320px"></div>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import * as echarts from 'echarts'
import { onMounted, ref } from 'vue'
import { analysisApi } from '@/api/analysisApi'
import { caseApi } from '@/api/caseApi'
import { categoryApi } from '@/api/categoryApi'
import { ruleApi } from '@/api/ruleApi'
import type { Category, RiskRule, ScamCase } from '@/types'

const categories = ref<Category[]>([])
const rules = ref<RiskRule[]>([])
const cases = ref<ScamCase[]>([])
const recentCount = ref(0)
const chartRef = ref<HTMLDivElement>()

onMounted(async () => {
  ;[categories.value, rules.value, cases.value] = await Promise.all([
    categoryApi.list(),
    ruleApi.list(),
    caseApi.list()
  ])
  recentCount.value = (await analysisApi.recent()).count
  const counts = categories.value.map((category) => ({
    name: category.name,
    value: rules.value.filter((rule) => rule.category_code === category.code).length
  }))
  if (chartRef.value) {
    echarts.init(chartRef.value).setOption({
      tooltip: { trigger: 'item' },
      series: [{ type: 'pie', radius: ['42%', '70%'], data: counts }]
    })
  }
})
</script>
