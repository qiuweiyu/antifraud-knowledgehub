<template>
  <section class="grid cols-2">
    <div class="panel">
      <h2>文本检测</h2>
      <el-input
        v-model="text"
        type="textarea"
        :rows="10"
        placeholder="输入可疑文本，例如：客服说账户异常，需要转账到安全账户"
        @input="clearSupplementalResult"
      />
      <div class="toolbar" style="margin-top: 14px">
        <el-button type="primary" :loading="loading" @click="analyze">分析风险</el-button>
        <el-button :disabled="!result" @click="copyJson">复制 JSON</el-button>
      </div>
      <p class="deterministic-note">规则引擎结果始终是主要风险判断；无需登录或启用 AI。</p>
    </div>

    <div class="panel">
      <el-empty v-if="!result" description="提交文本后显示风险分析结果" />
      <template v-else>
        <div class="primary-result-label">主要结果 · 规则引擎</div>
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

    <div class="panel assisted-panel">
      <div class="assisted-heading">
        <div>
          <h2>AI 辅助分析（可选）</h2>
          <p class="assisted-subtitle">AI 只提供补充说明，不修改风险分数、命中规则或建议动作。</p>
        </div>
        <el-tag type="warning" effect="plain">显式选择后才发送</el-tag>
      </div>

      <el-alert
        v-if="bridgeAvailable === false"
        title="当前部署未开启受控 AI 辅助入口"
        description="规则引擎仍可正常使用。AI 辅助需要同源 Browser Session 服务端配置。"
        type="info"
        :closable="false"
        show-icon
      />

      <template v-else-if="session">
        <div class="session-row">
          <div>
            <strong>受控会话已解锁</strong>
            <span class="session-meta">{{ session.display_label || session.principal_id }}</span>
          </div>
          <el-button size="small" :loading="logoutLoading" @click="logout">退出 AI 会话</el-button>
        </div>

        <el-alert
          v-if="profileMessage"
          :title="profileMessage"
          type="warning"
          :closable="false"
          show-icon
        />

        <template v-if="activeProfile">
          <div class="profile-card">
            <div>
              <strong>{{ activeProfile.display_name }}</strong>
              <div class="profile-meta">
                {{ activeProfile.provider_display_name }} · {{ activeProfile.model_display_name }}
              </div>
            </div>
            <el-tag type="success" effect="plain">服务器批准</el-tag>
          </div>

          <el-alert
            title="第三方数据传输提示"
            :description="activeProfile.disclosure"
            type="warning"
            :closable="false"
            show-icon
          />

          <div class="assisted-consent">
            <el-checkbox v-model="assistedOptIn">
              我明确选择使用 AI 辅助分析，并同意将本次输入按上方说明发送给服务器配置的第三方 AI 提供方。
            </el-checkbox>
          </div>

          <el-button
            type="warning"
            :loading="assistedLoading"
            :disabled="!assistedOptIn || !csrfToken"
            @click="runAssistedAnalysis"
          >
            运行 AI 辅助分析
          </el-button>
        </template>
      </template>

      <template v-else-if="bridgeAvailable !== false">
        <p>AI 辅助功能采用受控测试访问。Access Grant 只用于本次解锁请求，不会保存到浏览器存储。</p>
        <div class="unlock-row">
          <el-input
            v-model="accessGrant"
            type="password"
            autocomplete="off"
            name="browser-access-grant"
            placeholder="输入 Browser Access Grant"
            @keyup.enter="unlockSession"
          />
          <el-button type="primary" :loading="unlockLoading" @click="unlockSession">解锁 AI 会话</el-button>
        </div>
        <p v-if="sessionMessage" class="session-message">{{ sessionMessage }}</p>
      </template>

      <template v-if="assistedResult">
        <el-divider />
        <div class="supplemental-label">补充结果 · AI 辅助</div>
        <el-alert
          v-if="assistedResult.status === 'unavailable'"
          title="AI 辅助当前不可用"
          description="规则引擎主要结果不受影响。系统不会自动重试第三方 AI 请求。"
          type="warning"
          :closable="false"
          show-icon
        />
        <template v-else>
          <p class="assisted-summary">{{ assistedResult.assistance.summary }}</p>
          <template v-if="assistedResult.assistance.observations?.length">
            <h3>AI 补充观察</h3>
            <ul>
              <li v-for="item in assistedResult.assistance.observations" :key="item">{{ item }}</li>
            </ul>
          </template>
          <template v-if="assistedResult.assistance.limitations?.length">
            <h3>局限性</h3>
            <ul>
              <li v-for="item in assistedResult.assistance.limitations" :key="item">{{ item }}</li>
            </ul>
          </template>
        </template>
      </template>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { useRoute } from 'vue-router'
import { analysisApi } from '@/api/analysisApi'
import {
  browserAssistedApi,
  BrowserAssistedApiError
} from '@/api/browserAssistedApi'
import type {
  AnalysisResult,
  AssistedProfileMetadata,
  BrowserAssistedLLMResult,
  BrowserSession
} from '@/types'

const route = useRoute()
const text = ref('客服说我的账户异常，需要马上转账到安全账户验证。')
const loading = ref(false)
const result = ref<AnalysisResult>()

const bridgeAvailable = ref<boolean | null>(null)
const accessGrant = ref('')
const unlockLoading = ref(false)
const logoutLoading = ref(false)
const sessionMessage = ref('')
const session = ref<Omit<BrowserSession, 'csrf_token'>>()
const csrfToken = ref('')
const profiles = ref<AssistedProfileMetadata[]>([])
const activeProfile = ref<AssistedProfileMetadata>()
const profileMessage = ref('')
const assistedOptIn = ref(false)
const assistedLoading = ref(false)
const assistedResult = ref<BrowserAssistedLLMResult>()

const progressColor = computed(() =>
  (result.value?.risk_score || 0) >= 80
    ? '#dc2626'
    : (result.value?.risk_score || 0) >= 60
      ? '#f97316'
      : '#0f766e'
)

function clearSupplementalResult() {
  assistedResult.value = undefined
  assistedOptIn.value = false
}

function applySession(value: BrowserSession) {
  csrfToken.value = value.csrf_token
  session.value = {
    principal_id: value.principal_id,
    display_label: value.display_label,
    expires_at: value.expires_at
  }
}

function clearSessionState() {
  session.value = undefined
  csrfToken.value = ''
  profiles.value = []
  activeProfile.value = undefined
  profileMessage.value = ''
  assistedOptIn.value = false
  assistedResult.value = undefined
}

async function analyze() {
  loading.value = true
  assistedResult.value = undefined
  assistedOptIn.value = false
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

async function loadProfiles() {
  profiles.value = []
  activeProfile.value = undefined
  profileMessage.value = ''
  try {
    profiles.value = await browserAssistedApi.profiles()
    const available = profiles.value.filter((profile) => profile.availability === 'available')
    if (available.length === 1) {
      activeProfile.value = available[0]
      return
    }
    profileMessage.value = available.length === 0
      ? '当前没有可用的服务器批准 AI Profile。'
      : '服务器返回多个可用 AI Profile；在 Profile 选择器完成前，本页面不会自动选择。'
  } catch (error) {
    if (error instanceof BrowserAssistedApiError && error.status === 401) {
      clearSessionState()
      sessionMessage.value = 'AI 会话已失效，请重新解锁。'
      return
    }
    profileMessage.value = error instanceof Error ? error.message : 'AI Profile 加载失败'
  }
}

async function restoreSession() {
  try {
    const current = await browserAssistedApi.currentSession()
    bridgeAvailable.value = true
    sessionMessage.value = ''
    applySession(current)
    await loadProfiles()
  } catch (error) {
    clearSessionState()
    if (error instanceof BrowserAssistedApiError) {
      if (error.status === 401) {
        bridgeAvailable.value = true
        return
      }
      if (error.status === 404 || error.code === 'browser_assisted_cross_origin_blocked') {
        bridgeAvailable.value = false
        return
      }
    }
    bridgeAvailable.value = true
    sessionMessage.value = error instanceof Error ? error.message : 'AI 会话服务暂时不可用'
  }
}

async function unlockSession() {
  if (!accessGrant.value.trim()) {
    ElMessage.warning('请输入 Browser Access Grant')
    return
  }

  let grant = accessGrant.value
  accessGrant.value = ''
  unlockLoading.value = true
  sessionMessage.value = ''
  try {
    const created = await browserAssistedApi.exchangeSession(grant)
    bridgeAvailable.value = true
    applySession(created)
    await loadProfiles()
    ElMessage.success('AI 会话已解锁')
  } catch (error) {
    clearSessionState()
    if (error instanceof BrowserAssistedApiError && error.status === 404) {
      bridgeAvailable.value = false
    }
    sessionMessage.value = error instanceof Error ? error.message : 'AI 会话解锁失败'
  } finally {
    grant = ''
    unlockLoading.value = false
  }
}

async function logout() {
  if (!csrfToken.value) {
    clearSessionState()
    return
  }
  logoutLoading.value = true
  try {
    await browserAssistedApi.logout(csrfToken.value)
    clearSessionState()
    ElMessage.success('AI 会话已退出')
  } catch (error) {
    if (error instanceof BrowserAssistedApiError && error.status === 401) {
      clearSessionState()
    }
    ElMessage.error(error instanceof Error ? error.message : '退出 AI 会话失败')
  } finally {
    logoutLoading.value = false
  }
}

async function runAssistedAnalysis() {
  if (!assistedOptIn.value || !csrfToken.value || !activeProfile.value) {
    return
  }
  assistedLoading.value = true
  assistedResult.value = undefined
  try {
    const response = await browserAssistedApi.analyze(
      text.value,
      activeProfile.value.id,
      csrfToken.value
    )
    result.value = response.rule_result
    assistedResult.value = response.llm_assistance
    if (response.llm_assistance.status === 'unavailable') {
      ElMessage.warning('AI 辅助当前不可用，规则引擎结果已保留')
    }
  } catch (error) {
    if (error instanceof BrowserAssistedApiError && error.status === 401) {
      clearSessionState()
      sessionMessage.value = 'AI 会话已失效，请重新解锁。'
    } else if (error instanceof BrowserAssistedApiError && error.status === 429) {
      ElMessage.warning('AI 辅助请求已达到当前限额，请稍后手动重试')
    } else {
      ElMessage.error(error instanceof Error ? error.message : 'AI 辅助分析失败')
    }
  } finally {
    assistedLoading.value = false
  }
}

onMounted(() => {
  void restoreSession()
  if (route.query.demo === '1') {
    void analyze()
  }
})
</script>

<style scoped>
.assisted-panel {
  grid-column: 1 / -1;
}

.assisted-heading,
.session-row,
.profile-card,
.unlock-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
}

.assisted-heading h2 {
  margin-bottom: 4px;
}

.assisted-subtitle,
.deterministic-note,
.session-meta,
.profile-meta,
.session-message {
  color: #64748b;
}

.deterministic-note,
.assisted-subtitle,
.session-message {
  font-size: 13px;
}

.primary-result-label,
.supplemental-label {
  margin-bottom: 12px;
  font-size: 12px;
  font-weight: 700;
  letter-spacing: 0.08em;
  color: #475569;
}

.session-row,
.profile-card,
.assisted-consent {
  margin: 14px 0;
}

.session-meta,
.profile-meta {
  margin-left: 10px;
  font-size: 13px;
}

.profile-card {
  padding: 12px 14px;
  border: 1px solid #e2e8f0;
  border-radius: 8px;
}

.unlock-row {
  justify-content: flex-start;
}

.unlock-row .el-input {
  max-width: 560px;
}

.assisted-consent {
  padding: 12px 14px;
  border: 1px solid #fde68a;
  border-radius: 8px;
  background: #fffbeb;
}

.assisted-summary {
  font-weight: 600;
}
</style>
