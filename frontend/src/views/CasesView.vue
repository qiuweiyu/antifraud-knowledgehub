<template>
  <section>
    <div class="toolbar">
      <el-input v-model="keyword" placeholder="搜索标题/内容" style="max-width: 280px" />
      <el-select v-model="category" clearable placeholder="分类" style="width: 220px">
        <el-option v-for="item in categories" :key="item.code" :label="item.name" :value="item.code" />
      </el-select>
      <el-input v-model="tag" placeholder="标签筛选" style="max-width: 180px" />
    </div>
    <div class="grid cols-3">
      <article v-for="item in filtered" :key="item.id" class="case-card" @click="active = item">
        <el-tag size="small">{{ item.category_code }}</el-tag>
        <h3>{{ item.title }}</h3>
        <p class="muted">{{ item.summary }}</p>
        <el-tag v-for="risk in item.risk_points" :key="risk" type="warning" size="small">{{ risk }}</el-tag>
      </article>
    </div>
    <el-drawer v-model="drawerOpen" title="案例详情" size="42%">
      <template v-if="active">
        <h2>{{ active.title }}</h2>
        <p>{{ active.content }}</p>
        <el-divider />
        <p><strong>风险点：</strong>{{ active.risk_points.join('、') }}</p>
        <p><strong>标签：</strong>{{ active.tags.join('、') }}</p>
      </template>
    </el-drawer>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { caseApi } from '@/api/caseApi'
import { categoryApi } from '@/api/categoryApi'
import type { Category, ScamCase } from '@/types'

const cases = ref<ScamCase[]>([])
const categories = ref<Category[]>([])
const keyword = ref('')
const category = ref('')
const tag = ref('')
const active = ref<ScamCase>()
const drawerOpen = ref(false)

watch(active, (value) => { drawerOpen.value = Boolean(value) })

const filtered = computed(() => cases.value.filter((item) => {
  const text = `${item.title}${item.content}`
  return (!keyword.value || text.includes(keyword.value))
    && (!category.value || item.category_code === category.value)
    && (!tag.value || item.tags.some((value) => value.includes(tag.value)))
}))

onMounted(async () => {
  ;[cases.value, categories.value] = await Promise.all([caseApi.list(), categoryApi.list()])
})
</script>
