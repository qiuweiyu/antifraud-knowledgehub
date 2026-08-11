<template>
  <section class="grid">
    <div class="grid cols-4">
      <div class="metric"><strong>{{ stats?.categories ?? 0 }}</strong><span>诈骗分类</span></div>
      <div class="metric"><strong>{{ stats?.enabled_rules ?? 0 }}</strong><span>启用规则</span></div>
      <div class="metric"><strong>{{ stats?.cases ?? 0 }}</strong><span>匿名案例</span></div>
      <div class="metric"><strong>{{ stats?.analysis_records ?? 0 }}</strong><span>分析记录</span></div>
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
        <div v-loading="loading" ref="chartRef" style="height: 320px"></div>
      </div>
    </div>
    <div class="panel">
      <h2>分类规则与案例覆盖</h2>
      <el-table :data="stats?.category_distribution ?? []" size="small" border>
        <el-table-column prop="category_name" label="分类" min-width="160" />
        <el-table-column prop="category_code" label="代码" min-width="180" />
        <el-table-column prop="rule_count" label="规则数" width="100" />
        <el-table-column prop="case_count" label="案例数" width="100" />
      </el-table>
    </div>
  </section>
</template>

<script setup lang="ts">
import * as echarts from 'echarts'
import { onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { analysisApi } from '@/api/analysisApi'
import type { AnalysisStats } from '@/types'

const stats = ref<AnalysisStats>()
const loading = ref(false)
const chartRef = ref<HTMLDivElement>()

onMounted(async () => {
  loading.value = true
  try {
    stats.value = await analysisApi.stats()
    const counts = stats.value.category_distribution.map((category) => ({
      name: category.category_name,
      value: category.rule_count
    }))
    if (chartRef.value) {
      echarts.init(chartRef.value).setOption({
        tooltip: { trigger: 'item' },
        legend: { bottom: 0 },
        series: [{ name: '规则数', type: 'pie', radius: ['42%', '70%'], data: counts }]
      })
    }
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '统计数据加载失败')
  } finally {
    loading.value = false
  }
})
</script>
