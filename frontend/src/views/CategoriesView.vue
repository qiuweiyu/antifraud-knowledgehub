<template>
  <section class="grid cols-3">
    <article v-for="item in categories" :key="item.code" class="category-card">
      <el-tag>{{ item.severity_default }}</el-tag>
      <h3>{{ item.name }}</h3>
      <p class="muted">{{ item.description }}</p>
      <strong>{{ ruleCount(item.code) }} 条关联规则</strong>
    </article>
  </section>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { categoryApi } from '@/api/categoryApi'
import { ruleApi } from '@/api/ruleApi'
import type { Category, RiskRule } from '@/types'

const categories = ref<Category[]>([])
const rules = ref<RiskRule[]>([])

function ruleCount(code: string) {
  return rules.value.filter((rule) => rule.category_code === code).length
}

onMounted(async () => {
  ;[categories.value, rules.value] = await Promise.all([categoryApi.list(), ruleApi.list()])
})
</script>
