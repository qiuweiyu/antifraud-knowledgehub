<template>
  <section class="panel">
    <div class="toolbar">
      <el-input v-model="keyword" placeholder="搜索规则名称/描述" style="max-width: 260px" />
      <el-select v-model="category" clearable placeholder="分类" style="width: 220px">
        <el-option v-for="item in categories" :key="item.code" :label="item.name" :value="item.code" />
      </el-select>
      <el-select v-model="severity" clearable placeholder="风险等级" style="width: 160px">
        <el-option v-for="item in ['low','medium','high','critical']" :key="item" :label="item" :value="item" />
      </el-select>
    </div>
    <el-table v-loading="loading" :data="filtered" border>
      <el-table-column prop="name" label="名称" min-width="160" />
      <el-table-column prop="category_code" label="分类" min-width="150" />
      <el-table-column prop="rule_type" label="类型" width="130" />
      <el-table-column prop="severity" label="等级" width="110" />
      <el-table-column prop="weight" label="权重" width="80" />
      <el-table-column prop="explanation" label="解释" min-width="260" />
    </el-table>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { categoryApi } from '@/api/categoryApi'
import { ruleApi } from '@/api/ruleApi'
import type { Category, RiskRule } from '@/types'

const loading = ref(true)
const rules = ref<RiskRule[]>([])
const categories = ref<Category[]>([])
const keyword = ref('')
const category = ref('')
const severity = ref('')

const filtered = computed(() => rules.value.filter((rule) => {
  const textMatch = !keyword.value || `${rule.name}${rule.description}`.includes(keyword.value)
  return textMatch && (!category.value || rule.category_code === category.value) && (!severity.value || rule.severity === severity.value)
}))

onMounted(async () => {
  ;[rules.value, categories.value] = await Promise.all([ruleApi.list(), categoryApi.list()])
  loading.value = false
})
</script>
