import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const apiSource = await readFile(new URL('./src/api/browserAssistedApi.ts', import.meta.url), 'utf8')
const viewSource = await readFile(new URL('./src/views/AnalysisView.vue', import.meta.url), 'utf8')
const combined = `${apiSource}\n${viewSource}`

const forbiddenBrowserSecrets = [
  'LLM_ASSISTED_ANALYSIS_TOKEN',
  'OPENAI_API_KEY',
  'GEMINI_API_KEY',
  'DEEPSEEK_API_KEY',
  'localStorage',
  'sessionStorage',
  'indexedDB',
  'Authorization:'
]

test('browser assisted UX does not persist or embed privileged credentials', () => {
  for (const forbidden of forbiddenBrowserSecrets) {
    assert.equal(combined.includes(forbidden), false, `forbidden browser trust-boundary token found: ${forbidden}`)
  }
  assert.equal(/VITE_[A-Z0-9_]*(KEY|TOKEN|SECRET)/.test(combined), false)
})

test('browser assisted API is same-origin and uses only the browser bridge contracts', () => {
  assert.match(apiSource, /resolved\.origin !== window\.location\.origin/)
  assert.match(apiSource, /\/browser\/session\/exchange/)
  assert.match(apiSource, /\/browser\/session\/logout/)
  assert.match(apiSource, /\/browser\/analysis\/assisted\/profiles/)
  assert.match(apiSource, /\/browser\/analysis\/assisted'/)
  assert.match(apiSource, /'X-AFKH-CSRF'/)
  assert.doesNotMatch(apiSource, /provider\s*:/)
  assert.doesNotMatch(apiSource, /model\s*:/)
  assert.doesNotMatch(apiSource, /base_url|api_key|output_tokens|tools|retry/i)
})

test('access grant and CSRF remain transient UI state with explicit assisted opt in', () => {
  assert.match(viewSource, /accessGrant\.value = ''/)
  assert.match(viewSource, /csrfToken\.value = ''/)
  assert.match(viewSource, /v-model="assistedOptIn"/)
  assert.match(viewSource, /第三方数据传输提示/)
  assert.match(viewSource, /activeProfile\.disclosure/)
  assert.match(viewSource, /@click="runAssistedAnalysis"/)
})

test('deterministic analysis remains independent from assisted execution', () => {
  const analyzeFunction = viewSource.match(/async function analyze\(\) \{[\s\S]*?\n\}/)?.[0] || ''
  assert.match(analyzeFunction, /analysisApi\.analyzeText/)
  assert.doesNotMatch(analyzeFunction, /browserAssistedApi\.analyze/)

  const mountedBlock = viewSource.match(/onMounted\(\(\) => \{[\s\S]*?\n\}\)/)?.[0] || ''
  assert.doesNotMatch(mountedBlock, /browserAssistedApi\.analyze/)
})

test('single-profile I1 UX fails closed when more than one profile is available', () => {
  assert.match(viewSource, /if \(available\.length === 1\)/)
  assert.match(viewSource, /不会自动选择/)
})
