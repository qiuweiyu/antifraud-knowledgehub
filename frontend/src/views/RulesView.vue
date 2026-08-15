<template>
  <section class="panel">
    <div class="toolbar">
      <el-input v-model="keyword" placeholder="搜索规则名称/描述" style="max-width: 260px" />
      <el-select v-model="category" clearable placeholder="分类" style="width: 220px">
        <el-option v-for="item in categories" :key="item.code" :label="item.name" :value="item.code" />
      </el-select>
      <el-select v-model="severity" clearable placeholder="风险等级" style="width: 160px">
        <el-option v-for="item in severities" :key="item" :label="item" :value="item" />
      </el-select>
      <el-button type="primary" @click="openValidator">验证规则草稿</el-button>
    </div>

    <el-table v-loading="loading" :data="filtered" border>
      <el-table-column prop="name" label="名称" min-width="160" />
      <el-table-column prop="category_code" label="分类" min-width="150" />
      <el-table-column prop="rule_type" label="类型" width="150" />
      <el-table-column prop="severity" label="等级" width="110" />
      <el-table-column prop="weight" label="权重" width="80" />
      <el-table-column prop="explanation" label="解释" min-width="260" />
    </el-table>

    <el-dialog v-model="validatorVisible" title="验证规则草稿" width="720px" @closed="resetValidator">
      <el-form label-position="top">
        <div class="form-grid">
          <el-form-item label="Code" required>
            <el-input v-model="draft.code" placeholder="例如 community_safe_channel" />
          </el-form-item>
          <el-form-item label="名称" required>
            <el-input v-model="draft.name" placeholder="规则名称" />
          </el-form-item>
          <el-form-item label="分类" required>
            <el-select v-model="draft.category_code" placeholder="选择分类" style="width: 100%">
              <el-option v-for="item in categories" :key="item.code" :label="item.name" :value="item.code" />
            </el-select>
          </el-form-item>
          <el-form-item label="规则类型" required>
            <el-select v-model="draft.rule_type" placeholder="选择规则类型" style="width: 100%">
              <el-option v-for="item in ruleTypes" :key="item" :label="item" :value="item" />
            </el-select>
          </el-form-item>
          <el-form-item label="风险等级" required>
            <el-select v-model="draft.severity" placeholder="选择风险等级" style="width: 100%">
              <el-option v-for="item in severities" :key="item" :label="item" :value="item" />
            </el-select>
          </el-form-item>
          <el-form-item label="权重">
            <el-input-number v-model="draft.weight" :min="0" :max="100" style="width: 100%" />
          </el-form-item>
        </div>

        <el-form-item label="匹配模式" required>
          <el-input
            v-model="draft.pattern"
            type="textarea"
            :rows="3"
            placeholder="关键词、模式或正则表达式"
          />
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="draft.description" type="textarea" :rows="2" />
        </el-form-item>
        <el-form-item label="解释">
          <el-input v-model="draft.explanation" type="textarea" :rows="2" />
        </el-form-item>
        <el-form-item label="建议">
          <el-input v-model="draft.recommendation" type="textarea" :rows="2" />
        </el-form-item>
      </el-form>

      <el-alert
        v-if="validationResult?.valid"
        title="规则草稿验证通过"
        type="success"
        :closable="false"
        show-icon
      />

      <div v-if="validationResult && !validationResult.valid" class="validation-result">
        <el-alert
          title="规则草稿存在校验错误"
          type="error"
          :closable="false"
          show-icon
        />
        <ul class="validation-list">
          <li v-for="item in validationResult.errors" :key="`${item.field}-${item.code}`">
            <strong>{{ item.field }}</strong> — {{ item.message }} <code>{{ item.code }}</code>
          </li>
        </ul>
      </div>

      <div v-if="validationResult?.warnings.length" class="validation-result">
        <el-alert title="校验警告" type="warning" :closable="false" show-icon />
        <ul class="validation-list">
          <li v-for="item in validationResult.warnings" :key="`${item.field}-${item.code}`">
            <strong>{{ item.field }}</strong> — {{ item.message }} <code>{{ item.code }}</code>
          </li>
        </ul>
      </div>

      <el-alert
        v-if="validationError"
        :title="validationError"
        type="error"
        :closable="false"
        show-icon
      />

      <template #footer>
        <el-button @click="resetDraft">重置</el-button>
        <el-button type="primary" :loading="validating" @click="validateDraft">Validate</el-button>
      </template>
    </el-dialog>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { categoryApi } from '@/api/categoryApi'
import { ruleApi } from '@/api/ruleApi'
import type { RuleDraft, RuleValidationResult } from '@/api/ruleApi'
import type { Category, RiskRule } from '@/types'

const ruleTypes: RuleDraft['rule_type'][] = ['keyword', 'pattern', 'semantic_placeholder', 'regex']
const severities: RuleDraft['severity'][] = ['low', 'medium', 'high', 'critical']

const loading = ref(true)
const rules = ref<RiskRule[]>([])
const categories = ref<Category[]>([])
const keyword = ref('')
const category = ref('')
const severity = ref('')
const validatorVisible = ref(false)
const validating = ref(false)
const validationResult = ref<RuleValidationResult | null>(null)
const validationError = ref('')

const emptyDraft = (): RuleDraft => ({
  code: '',
  name: '',
  description: '',
  category_code: '',
  rule_type: 'keyword',
  pattern: '',
  weight: 20,
  severity: 'medium',
  explanation: '',
  recommendation: ''
})

const draft = reactive<RuleDraft>(emptyDraft())

const filtered = computed(() => rules.value.filter((rule) => {
  const textMatch = !keyword.value || `${rule.name}${rule.description}`.includes(keyword.value)
  return textMatch && (!category.value || rule.category_code === category.value) && (!severity.value || rule.severity === severity.value)
}))

function openValidator() {
  validatorVisible.value = true
}

function resetDraft() {
  Object.assign(draft, emptyDraft())
  validationResult.value = null
  validationError.value = ''
}

function resetValidator() {
  resetDraft()
  validating.value = false
}

async function validateDraft() {
  validating.value = true
  validationResult.value = null
  validationError.value = ''
  try {
    validationResult.value = await ruleApi.validate({ ...draft })
  } catch (error) {
    validationError.value = error instanceof Error ? error.message : '规则草稿验证请求失败'
  } finally {
    validating.value = false
  }
}

onMounted(async () => {
  try {
    ;[rules.value, categories.value] = await Promise.all([ruleApi.list(), categoryApi.list()])
  } finally {
    loading.value = false
  }
})
</script>

<style scoped>
.form-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0 16px;
}

.validation-result {
  margin-top: 16px;
}

.validation-list {
  margin: 12px 0 0;
  padding-left: 20px;
}

.validation-list li + li {
  margin-top: 8px;
}

@media (max-width: 720px) {
  .form-grid {
    grid-template-columns: 1fr;
  }
}
</style>
