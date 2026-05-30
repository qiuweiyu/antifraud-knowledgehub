import { createRouter, createWebHistory } from 'vue-router'
import MainLayout from '@/layouts/MainLayout.vue'
import OverviewView from '@/views/OverviewView.vue'
import AnalysisView from '@/views/AnalysisView.vue'
import RulesView from '@/views/RulesView.vue'
import CasesView from '@/views/CasesView.vue'
import CategoriesView from '@/views/CategoriesView.vue'
import AboutView from '@/views/AboutView.vue'

export default createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: '/',
      component: MainLayout,
      children: [
        { path: '', name: 'overview', component: OverviewView },
        { path: 'analysis', name: 'analysis', component: AnalysisView },
        { path: 'rules', name: 'rules', component: RulesView },
        { path: 'cases', name: 'cases', component: CasesView },
        { path: 'categories', name: 'categories', component: CategoriesView },
        { path: 'about', name: 'about', component: AboutView }
      ]
    }
  ]
})
