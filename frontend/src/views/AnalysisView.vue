<template>
  <section class="grid cols-2">
    <div class="panel">
      <h2>文本检测</h2>
      <el-input v-model="text" type="textarea" :rows="10" placeholder="输入可疑文本，例如：客服说账户异常，需要转账到安全账户" />
      <div class="toolbar" style="margin-top: 14px">
        <el-button type="primary" :loading="loading" @click="analyze">分析风险</el-button>
        <el-button :disabled="!result" @click="copyJson">复制 JSON</el-button>
      </div>
    </div>
    <div class="panel">
      <el-empty v-if="!result" description="提交文本后显示风险分析结果" />
      <template v-else>
        <el-progress type="dashboard" :percentage="result.risk_score" :color="progressColor" />
        <h2 :class="`risk-${result.risk_level}`">{{ result.risk_level.toUpperCase() }}</h2>
        <p>{{ result.summary }}</p>
        <el-divider />
        <h3>命中规则</h3>
        <el-table :data="result.matched_rules" size="small">
          <el-table-column prop="rule_name" label="规则" />
          <el-table-column prop="evidence" label="证据" />
          <el-table-column prop="weight" label="权重" width="70" />
        </el-table>
        <h3>建议动作</h3>
        <ul><li v-for="item in result.recommendations" :key="item">{{ item }}</li></ul>
      </template>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { analysisApi } from '@/api/analysisApi'
import type { AnalysisResult } from '@/types'

const text = ref('客服说我的账户异常，需要马上转账到安全账户验证。')
const loading = ref(false)
const result = ref<AnalysisResult>()
const progressColor = computed(() => (result.value?.risk_score || 0) >= 80 ? '#dc2626' : (result.value?.risk_score || 0) >= 60 ? '#f97316' : '#0f766e')

async function analyze() {
  loading.value = true
  try {
    result.value = await analysisApi.analyzeText(text.value)
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '分析失败')
  } finally {
    loading.value = false
  }
}

async function copyJson() {
  await navigator.clipboard.writeText(JSON.stringify(result.value, null, 2))
  ElMessage.success('JSON 已复制')
}
</script>
