<template>
  <div class="app-shell">
    <aside class="sidebar">
      <div class="brand">
        <div class="brand-mark">AF</div>
        <div>
          <strong>AntiFraud</strong>
          <span>KnowledgeHub</span>
        </div>
      </div>
      <nav>
        <RouterLink v-for="item in nav" :key="item.path" :to="item.path">{{ item.label }}</RouterLink>
      </nav>
    </aside>
    <main class="content">
      <header class="topbar">
        <div>
          <h1>{{ routeTitle }}</h1>
          <p>面向中文互联网场景的可解释反诈骗知识库</p>
        </div>
        <el-tag type="success" effect="dark">Early-stage OSS</el-tag>
      </header>
      <RouterView />
    </main>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'

const route = useRoute()
const nav = [
  { path: '/', label: 'Overview' },
  { path: '/analysis', label: 'Text Analysis' },
  { path: '/rules', label: 'Risk Rules' },
  { path: '/cases', label: 'Scam Cases' },
  { path: '/categories', label: 'Categories' },
  { path: '/about', label: 'About / Docs' }
]

const routeTitle = computed(() => nav.find((item) => item.path === route.path)?.label || 'Overview')
</script>
