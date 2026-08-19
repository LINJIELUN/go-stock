<script setup>
import {computed, onMounted, ref} from 'vue'
import {NAlert, NButton, NCard, NEmpty, NGrid, NGridItem, NSelect, NSpin, NStatistic, NTag} from 'naive-ui'
import {GetAIShadowReport, ListAIShadowCohorts} from '../../wailsjs/go/main/App'

const DAY_MS = 24 * 60 * 60 * 1000
const loading = ref(false)
const errorMessage = ref('')
const cohorts = ref([])
const selectedKey = ref(null)
const report = ref(null)

const windowEnd = new Date()
const windowStart = new Date(windowEnd.getTime() - 90 * DAY_MS)

function cohortKey(cohort) {
  return [cohort.strategyVersion, cohort.modelVersion, cohort.promptVersion].join('\u0000')
}

const cohortOptions = computed(() => cohorts.value.map((cohort) => ({
  label: `${cohort.strategyVersion} · ${cohort.modelVersion} · ${cohort.promptVersion}（${cohort.generatedSnapshots} 条）`,
  value: cohortKey(cohort),
})))

const selectedCohort = computed(() => cohorts.value.find((cohort) => cohortKey(cohort) === selectedKey.value))

function metric(value, suffix = '') {
  return value === null || value === undefined ? '暂无数据' : `${Number(value).toFixed(2)}${suffix}`
}

async function loadReport() {
  const cohort = selectedCohort.value
  if (!cohort) {
    report.value = null
    return
  }
  loading.value = true
  errorMessage.value = ''
  try {
    report.value = await GetAIShadowReport(
      windowStart.toISOString(), windowEnd.toISOString(),
      cohort.strategyVersion, cohort.modelVersion, cohort.promptVersion,
    )
  } catch (error) {
    report.value = null
    errorMessage.value = error?.message || String(error)
  } finally {
    loading.value = false
  }
}

async function loadCohorts() {
  loading.value = true
  errorMessage.value = ''
  try {
    cohorts.value = await ListAIShadowCohorts(windowStart.toISOString(), windowEnd.toISOString())
    selectedKey.value = cohorts.value.length ? cohortKey(cohorts.value[0]) : null
    await loadReport()
  } catch (error) {
    cohorts.value = []
    selectedKey.value = null
    report.value = null
    errorMessage.value = error?.message || String(error)
  } finally {
    loading.value = false
  }
}

onMounted(loadCohorts)
</script>

<template>
  <main class="shadow-page">
    <header class="shadow-header">
      <div>
        <div class="eyebrow">AI RECOMMENDATION AUDIT</div>
        <h1>影子运行报告</h1>
        <p>仅展示最近 90 天同一策略、模型与提示词版本的可核验证据，不执行自动晋级。</p>
      </div>
      <n-button secondary :loading="loading" @click="loadCohorts">刷新证据</n-button>
    </header>

    <n-alert type="warning" :show-icon="true" class="boundary-alert">
      “暂无数据”表示尚无已完成复盘，不等于 0% 命中率。是否正式开放仍需单独的产品与风险决策。
    </n-alert>

    <section class="cohort-toolbar">
      <div>
        <span class="toolbar-label">报告配置</span>
        <n-select
          v-model:value="selectedKey"
          :options="cohortOptions"
          :disabled="loading || cohortOptions.length === 0"
          placeholder="当前窗口没有可报告配置"
          @update:value="loadReport"
        />
      </div>
      <n-tag v-if="selectedCohort" type="info" :bordered="false">
        样本 {{ selectedCohort.generatedSnapshots }} · {{ new Date(selectedCohort.firstCompletedAt).toLocaleDateString() }}—{{ new Date(selectedCohort.lastCompletedAt).toLocaleDateString() }}
      </n-tag>
    </section>

    <n-alert v-if="errorMessage" type="error" class="error-alert">{{ errorMessage }}</n-alert>

    <n-spin :show="loading">
      <n-empty v-if="!loading && !report" description="最近 90 天没有影子运行证据" class="empty-state" />
      <template v-else-if="report">
        <n-grid cols="1 s:2 l:4" responsive="screen" :x-gap="16" :y-gap="16" class="metric-grid">
          <n-grid-item><n-card><n-statistic label="生成快照" :value="report.generatedSnapshots" /></n-card></n-grid-item>
          <n-grid-item><n-card><n-statistic label="已完成复盘" :value="report.completedReviews" /></n-card></n-grid-item>
          <n-grid-item><n-card><n-statistic label="延期复盘" :value="report.delayedReviews" /></n-card></n-grid-item>
          <n-grid-item><n-card><n-statistic label="待复盘" :value="report.pendingReviews" /></n-card></n-grid-item>
          <n-grid-item><n-card><n-statistic label="方向命中率" :value="metric(report.directionHitRatePercent, '%')" /></n-card></n-grid-item>
          <n-grid-item><n-card><n-statistic label="区间命中率" :value="metric(report.rangeHitRatePercent, '%')" /></n-card></n-grid-item>
          <n-grid-item><n-card><n-statistic label="平均实际收益" :value="metric(report.meanActualReturnPercent, '%')" /></n-card></n-grid-item>
          <n-grid-item><n-card><n-statistic label="平均区间外偏差" :value="metric(report.meanOutsideDeviation)" /></n-card></n-grid-item>
        </n-grid>

        <n-card title="模型费用证据" class="cost-card">
          <div class="cost-row"><span>已结算费用</span><strong>${{ Number(report.settledModelCostUsd).toFixed(6) }}</strong></div>
          <div class="cost-row"><span>当前承诺费用</span><strong>${{ Number(report.committedModelCostUsd).toFixed(6) }}</strong></div>
          <div class="cost-row"><span>费用不确定调用</span><strong>{{ report.uncertainModelUsageCount }}</strong></div>
        </n-card>
      </template>
    </n-spin>
  </main>
</template>

<style scoped>
.shadow-page { min-height: 100%; padding: 28px; background: #f6f8fb; color: #172033; }
.shadow-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 24px; margin-bottom: 20px; }
.shadow-header h1 { margin: 4px 0 8px; font-size: 30px; letter-spacing: -.03em; }
.shadow-header p { margin: 0; color: #667085; }
.eyebrow { color: #1677ff; font-size: 12px; font-weight: 700; letter-spacing: .14em; }
.boundary-alert, .error-alert { margin-bottom: 18px; }
.cohort-toolbar { display: grid; grid-template-columns: minmax(320px, 1fr) auto; align-items: end; gap: 16px; padding: 18px; margin-bottom: 18px; background: #fff; border: 1px solid #e7ebf1; border-radius: 12px; }
.toolbar-label { display: block; margin-bottom: 8px; color: #475467; font-size: 13px; font-weight: 600; }
.metric-grid :deep(.n-card) { height: 100%; border-radius: 12px; }
.cost-card { margin-top: 18px; border-radius: 12px; }
.cost-row { display: flex; justify-content: space-between; padding: 10px 0; border-bottom: 1px solid #eef1f5; }
.cost-row:last-child { border-bottom: 0; }
.empty-state { padding: 72px 0; }
@media (max-width: 720px) { .shadow-page { padding: 18px; } .shadow-header { flex-direction: column; } .cohort-toolbar { grid-template-columns: 1fr; } }
</style>
