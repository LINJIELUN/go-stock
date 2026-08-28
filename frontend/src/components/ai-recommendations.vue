<script setup>
import {computed, onMounted, ref} from 'vue'
import {NAlert, NButton, NCard, NDataTable, NDescriptions, NDescriptionsItem, NEmpty, NInput, NInputGroup, NModal, NProgress, NTabPane, NTabs, NTag} from 'naive-ui'
import {GetAIRecommendationCards, SearchAIRecommendationCards, SetAIRecommendationFavorite} from '../../wailsjs/go/main/App'

const activeTab = ref('today')
const query = ref('')
const analyzing = ref(false)
const analyzedStock = ref(null)
const searchMessage = ref('')
const selectedStock = ref(null)
const loadingRecords = ref(false)
const recordError = ref('')
const isDesktopRuntime = Boolean(window.go?.main?.App?.GetAIRecommendationCards)

// 浏览器预览保留少量固定记录；桌面应用从首帧起只展示本地真实记录。
const demoRecommendations = [
  {code: '600519', name: '贵州茅台', price: 1486.32, probability: 68, range: '+2.4% ～ +7.1%', index: 76, level: '高度关注', tags: ['沪市'], reason: '盈利质量稳定，近期波动收敛，趋势因子保持正向。', risk: '消费复苏不及预期；估值仍处于较高区间。', completedAt: '2026-08-20 16:08'},
  {code: '300750', name: '宁德时代', price: 268.45, probability: 63, range: '-1.8% ～ +6.2%', index: 69, level: '值得关注', tags: ['创业板'], reason: '成交活跃度较好，产业链信息偏正面，但预测区间较宽。', risk: '行业价格竞争和海外政策变化可能放大波动。', completedAt: '2026-08-20 16:21'},
  {code: '159915', name: '创业板ETF', price: 2.146, probability: 57, range: '-2.5% ～ +4.0%', index: 58, level: '谨慎观察', tags: ['ETF'], reason: '分散单股风险，短期趋势尚未形成强一致性。', risk: '指数波动与市场风险偏好高度相关。', completedAt: '2026-08-20 16:34'},
  {code: '920799', name: '艾融软件', price: 42.18, probability: 54, range: '-5.2% ～ +8.6%', index: 46, level: '谨慎观察', tags: ['北交所', '新股'], reason: '成长性具备观察价值，但历史样本和流动性证据有限。', risk: '新股样本不足、波动较高，预测不确定性明显。', completedAt: '2026-08-20 16:48'},
]

const demoReviews = [
  {code: '600519', name: '贵州茅台', frozenAt: '2026-08-11 16:12', probability: 66, predictedRange: '+1.5% ～ +6.0%', index: 73, dueDate: '2026-08-20', actualReturn: '+3.82%', direction: '命中', interval: '命中', deviation: '0.00%', status: 'completed'},
  {code: '300750', name: '宁德时代', frozenAt: '2026-08-18 16:28', probability: 62, predictedRange: '-2.0% ～ +5.8%', index: 67, dueDate: '2026-08-27', actualReturn: '待复盘', direction: '—', interval: '—', deviation: '—', status: 'pending'},
]
const dataMode = ref(isDesktopRuntime ? '本地推荐记录' : '演示数据')
const recommendations = ref(isDesktopRuntime ? [] : demoRecommendations)
const favoriteCodes = ref(new Set(isDesktopRuntime ? [] : ['600519', '300750']))
const reviews = ref(isDesktopRuntime ? [] : demoReviews)

const favoriteRows = computed(() => reviews.value.filter(item => favoriteCodes.value.has(item.code)))
const tagType = tag => tag.includes('ST') || tag === '新股' ? 'warning' : tag === 'ETF' ? 'info' : 'default'
const indexColor = value => value >= 75 ? '#db5b83' : value >= 60 ? '#ed8aa8' : '#d9a4b3'
const latestDataTime = computed(() => recommendations.value.length ? recommendations.value[0].completedAt : '暂无数据')
const hitText = value => value === undefined || value === null ? '待复盘' : value ? '命中' : '未命中'
const valueText = (value, suffix = '') => value === undefined || value === null ? '待复盘' : `${Number(value).toFixed(2)}${suffix}`

async function toggleFavorite(stock) {
  const next = new Set(favoriteCodes.value)
  const favorite = !next.has(stock.code)
  favorite ? next.add(stock.code) : next.delete(stock.code)
  favoriteCodes.value = next
  if (!reviews.value.some(item => item.code === stock.code)) {
    reviews.value.push({code: stock.code, name: stock.name, frozenAt: stock.completedAt, probability: stock.probability, predictedRange: stock.range, index: stock.index, dueDate: '待计算', actualReturn: '待复盘', direction: '—', interval: '—', deviation: '—', status: 'pending'})
  }
  if (stock.id) {
    try {
      await SetAIRecommendationFavorite(stock.id, favorite)
    } catch (error) {
      favorite ? next.delete(stock.code) : next.add(stock.code)
      favoriteCodes.value = new Set(next)
      recordError.value = error?.message || String(error)
    }
  }
}

function percent(value) { return value === null || value === undefined ? null : Number(value) }
function backendCard(card) {
  return {id: card.id, code: card.stockCode, name: card.stockName, price: card.baselinePrice,
    probability: Number(card.riseProbability), range: `${Number(card.returnRangeLow).toFixed(1)}% ～ ${Number(card.returnRangeHigh).toFixed(1)}%`,
    index: card.aiRecommendationIndex, level: card.aiRecommendationIndex >= 75 ? '高度关注' : card.aiRecommendationIndex >= 60 ? '值得关注' : '谨慎观察',
    tags: card.riskLabels || [], reason: card.rationale || '暂无研究摘要', risk: card.riskNotes || '暂无风险说明',
    completedAt: new Date(card.completedAt).toLocaleString(), isFavorite: card.isFavorite,
    dueDate: new Date(card.reviewDueDate).toLocaleDateString(), actualReturn: percent(card.actualReturnPercent),
    directionHit: card.directionHit, rangeHit: card.rangeHit, deviation: percent(card.outsideRangeDeviation), reviewStatus: card.reviewStatus}
}

async function loadPersistedRecords() {
  if (!isDesktopRuntime) return
  loadingRecords.value = true
  recordError.value = ''
  try {
    const cards = (await GetAIRecommendationCards(100, false)).map(backendCard)
    dataMode.value = '本地推荐记录'
    recommendations.value = cards
    favoriteCodes.value = new Set(cards.filter(card => card.isFavorite).map(card => card.code))
    reviews.value = cards.filter(card => card.isFavorite).map(card => ({code: card.code, name: card.name, frozenAt: card.completedAt,
      probability: card.probability, predictedRange: card.range, index: card.index, dueDate: card.dueDate,
      actualReturn: card.actualReturn === null ? '待复盘' : `${card.actualReturn.toFixed(2)}%`, direction: card.directionHit === undefined || card.directionHit === null ? '—' : card.directionHit ? '命中' : '未命中',
      interval: card.rangeHit === undefined || card.rangeHit === null ? '—' : card.rangeHit ? '命中' : '未命中', deviation: card.deviation === null ? '—' : `${card.deviation.toFixed(2)}%`, status: card.reviewStatus}))
  } catch (error) {
    recordError.value = error?.message || String(error)
  } finally { loadingRecords.value = false }
}

onMounted(loadPersistedRecords)

async function analyze() {
  if (!query.value.trim()) return
  analyzing.value = true
  analyzedStock.value = null
  searchMessage.value = ''
  try {
    if (isDesktopRuntime) {
      const results = await SearchAIRecommendationCards(query.value.trim(), 10)
      analyzedStock.value = results.length ? backendCard(results[0]) : null
      if (!results.length) searchMessage.value = '本地没有这只股票的历史分析。真实模型尚未配置时不会生成虚构结果。'
    } else {
      await new Promise(resolve => setTimeout(resolve, 350))
      const existing = recommendations.value.find(item => item.code === query.value.trim() || item.name.includes(query.value.trim()))
      analyzedStock.value = existing || null
      if (!existing) searchMessage.value = '演示数据中没有匹配记录。'
    }
  } catch (error) {
    searchMessage.value = error?.message || String(error)
  } finally { analyzing.value = false }
}

const reviewColumns = [
  {title: '股票', key: 'stock', render: row => `${row.name} · ${row.code}`},
  {title: '收藏时预测', key: 'prediction', render: row => `${row.probability}% · ${row.predictedRange}`},
  {title: 'AI推荐指数', key: 'index'},
  {title: '到期日', key: 'dueDate'},
  {title: '实际涨幅', key: 'actualReturn'},
  {title: '方向', key: 'direction'},
  {title: '区间', key: 'interval'},
  {title: '区间外偏差', key: 'deviation'},
]
</script>

<template>
  <main class="recommend-page">
    <header class="page-header">
      <div><span class="eyebrow">POST-CLOSE RESEARCH</span><h1>AI 股票研究</h1><p>收盘后推荐、单股分析、收藏快照与七交易日复盘</p></div>
      <n-tag round :bordered="false" class="market-tag">{{ dataMode }} · A 股收盘后</n-tag>
    </header>

    <n-alert v-if="dataMode === '演示数据'" type="warning" :show-icon="true" class="demo-alert">
      当前页面使用固定测试数据验证产品交互，概率标注为模型估计、非实际结果，不构成投资建议。
    </n-alert>
    <n-alert v-else type="info" :show-icon="true" class="demo-alert">本页读取本机数据库中的推荐与复盘记录；上涨概率仍是模型估计、非实际结果。</n-alert>
    <n-alert v-if="recordError" type="error" class="demo-alert">读取或保存本地推荐记录失败：{{ recordError }}</n-alert>

    <n-tabs v-model:value="activeTab" type="segment" animated class="product-tabs">
      <n-tab-pane name="today" tab="今日推荐">
        <div class="section-heading"><div><h2>收盘后精选</h2><p>目标 10～20 只；证据不足时不强行凑数</p></div><span class="updated">最近记录 {{ latestDataTime }}</span></div>
        <n-empty v-if="!loadingRecords && recommendations.length === 0" description="本地还没有推荐记录" class="manual-empty" />
        <section v-else class="recommend-grid">
          <n-card v-for="stock in recommendations" :key="stock.code" class="stock-card" :bordered="false">
            <div class="stock-top"><div><h3>{{ stock.name }}</h3><span>{{ stock.code }} · ¥{{ stock.price }}</span></div><n-button quaternary circle class="favorite" @click="toggleFavorite(stock)">{{ favoriteCodes.has(stock.code) ? '♥' : '♡' }}</n-button></div>
            <div class="tags"><n-tag v-for="tag in stock.tags" :key="tag" size="small" :type="tagType(tag)" round>{{ tag }}</n-tag></div>
            <div class="prediction"><div><span>7日上涨概率</span><strong>{{ stock.probability }}%</strong><small>模型估计、非实际结果</small></div><div><span>预计涨跌区间</span><strong>{{ stock.range }}</strong><small>第七交易日收盘</small></div></div>
            <div class="index-row"><div><span>AI推荐指数</span><b>{{ stock.index }}</b><small>{{ stock.level }}</small></div><n-progress type="line" :percentage="stock.index" :show-indicator="false" :color="indexColor(stock.index)" rail-color="#f8e8ed" /></div>
            <p class="reason"><b>研究摘要</b>{{ stock.reason }}</p><p class="risk"><b>主要风险</b>{{ stock.risk }}</p>
            <div class="card-footer"><span>{{ stock.completedAt }}</span><n-button text color="#cc557b" @click="selectedStock = stock">查看分析详情 →</n-button></div>
          </n-card>
        </section>
      </n-tab-pane>

      <n-tab-pane name="manual" tab="手动分析">
        <section class="manual-panel"><div class="manual-copy"><span class="eyebrow">SINGLE STOCK</span><h2>搜索一只 A 股</h2><p>{{ isDesktopRuntime ? '查询本机已经保存的历史分析；真实模型入口将在数据与模型配置完成后启用。' : '预览模式仅演示搜索与结果展示，不会调用真实模型。' }}</p></div><n-input-group><n-input v-model:value="query" size="large" placeholder="输入代码或名称，例如 600519" clearable @keyup.enter="analyze"/><n-button size="large" type="primary" color="#d75f83" :loading="analyzing" @click="analyze">{{ isDesktopRuntime ? '查询历史分析' : '演示搜索' }}</n-button></n-input-group></section>
        <n-empty v-if="!analyzedStock && !analyzing" description="搜索结果将在这里展示上涨概率、预计区间和 AI 推荐指数" class="manual-empty" />
        <n-alert v-if="searchMessage" type="warning" class="manual-message">{{ searchMessage }}</n-alert>
        <n-card v-if="analyzedStock" class="manual-result" :bordered="false"><div class="stock-top"><div><h3>{{ analyzedStock.name }}</h3><span>{{ analyzedStock.code }} · ¥{{ analyzedStock.price }}</span></div><n-button secondary round color="#cc557b" @click="toggleFavorite(analyzedStock)">{{ favoriteCodes.has(analyzedStock.code) ? '已收藏 ♥' : '收藏 ♡' }}</n-button></div><div class="result-metrics"><div><span>7日上涨概率</span><strong>{{ analyzedStock.probability }}%</strong></div><div><span>预计涨跌区间</span><strong>{{ analyzedStock.range }}</strong></div><div><span>AI推荐指数</span><strong>{{ analyzedStock.index }}</strong></div></div><n-alert type="info" :show-icon="false">{{ analyzedStock.reason }} 风险：{{ analyzedStock.risk }}</n-alert></n-card>
      </n-tab-pane>

      <n-tab-pane name="favorites" tab="收藏与复盘">
        <div class="section-heading"><div><h2>推荐收藏快照</h2><p>取消收藏只移除关系，原预测和复盘历史继续保留</p></div></div>
        <n-data-table :columns="reviewColumns" :data="favoriteRows" :bordered="false" class="review-table" />
      </n-tab-pane>
    </n-tabs>

    <n-modal :show="!!selectedStock" preset="card" :bordered="false" class="detail-modal" style="width:min(760px,calc(100vw - 32px))" @update:show="value => { if (!value) selectedStock = null }">
      <template #header><div v-if="selectedStock"><strong>{{ selectedStock.name }}</strong><small>{{ selectedStock.code }} · 推荐快照详情</small></div></template>
      <template v-if="selectedStock">
        <div class="detail-hero"><div><span>7日上涨概率</span><b>{{ selectedStock.probability }}%</b><small>模型估计、非实际结果</small></div><div><span>预计涨跌区间</span><b>{{ selectedStock.range }}</b><small>相对推荐基准价</small></div><div><span>AI推荐指数</span><b>{{ selectedStock.index }}</b><small>{{ selectedStock.level }}</small></div></div>
        <div class="detail-tags"><n-tag v-for="tag in selectedStock.tags" :key="tag" size="small" :type="tagType(tag)" round>{{ tag }}</n-tag></div>
        <n-card title="分析结论" size="small" class="detail-section"><p><b>研究摘要</b>{{ selectedStock.reason }}</p><p><b>风险因素</b>{{ selectedStock.risk }}</p></n-card>
        <n-descriptions label-placement="top" :column="3" bordered class="detail-section">
          <n-descriptions-item label="推荐基准价">¥{{ selectedStock.price }}</n-descriptions-item><n-descriptions-item label="分析完成时间">{{ selectedStock.completedAt }}</n-descriptions-item><n-descriptions-item label="计划复盘日">{{ selectedStock.dueDate || '待计算' }}</n-descriptions-item>
          <n-descriptions-item label="实际涨跌幅">{{ valueText(selectedStock.actualReturn, '%') }}</n-descriptions-item><n-descriptions-item label="方向判断">{{ hitText(selectedStock.directionHit) }}</n-descriptions-item><n-descriptions-item label="区间判断">{{ hitText(selectedStock.rangeHit) }}</n-descriptions-item>
          <n-descriptions-item label="区间外偏差">{{ valueText(selectedStock.deviation, '%') }}</n-descriptions-item><n-descriptions-item label="记录类型">{{ selectedStock.id ? '本地持久化记录' : '交互演示记录' }}</n-descriptions-item><n-descriptions-item label="收藏状态">{{ favoriteCodes.has(selectedStock.code) ? '已收藏' : '未收藏' }}</n-descriptions-item>
        </n-descriptions>
        <div class="detail-actions"><n-button secondary @click="selectedStock = null">关闭</n-button><n-button type="primary" color="#d75f83" @click="toggleFavorite(selectedStock)">{{ favoriteCodes.has(selectedStock.code) ? '取消收藏' : '收藏快照' }}</n-button></div>
      </template>
    </n-modal>
  </main>
</template>

<style scoped>
.recommend-page{min-height:100%;padding:28px;background:linear-gradient(145deg,#fffafa 0%,#fff5f7 55%,#fdf1f5 100%);color:#39252d}.page-header,.section-heading,.stock-top,.card-footer{display:flex;align-items:center;justify-content:space-between;gap:16px}.page-header{margin-bottom:18px}.page-header h1{margin:5px 0 4px;font-size:30px}.page-header p,.section-heading p,.manual-copy p{margin:0;color:#8a6a75}.eyebrow{font-size:11px;font-weight:800;letter-spacing:.16em;color:#d45f83}.market-tag{background:#f8dce5;color:#a83e61}.demo-alert{margin-bottom:18px;border-radius:12px}.product-tabs{--n-tab-color-segment:#f8e6ec}.section-heading{margin:22px 0 16px}.section-heading h2,.manual-copy h2{margin:0 0 5px}.updated{font-size:12px;color:#a77c8b}.recommend-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:16px}.stock-card,.manual-result{border:1px solid #f1dce3;border-radius:18px;background:rgba(255,255,255,.94);box-shadow:0 10px 30px rgba(155,74,101,.08)}.stock-top{align-items:flex-start}.stock-top h3{margin:0 0 3px;font-size:20px}.stock-top span{font-size:13px;color:#97717f}.favorite{font-size:24px;color:#cf5479}.tags,.detail-tags{display:flex;gap:6px;margin:12px 0}.prediction,.result-metrics{display:grid;grid-template-columns:repeat(2,1fr);gap:10px}.prediction>div,.result-metrics>div{padding:13px;border-radius:12px;background:#fff5f7}.prediction span,.prediction small,.result-metrics span{display:block;color:#96717f;font-size:12px}.prediction strong,.result-metrics strong{display:block;margin:4px 0;color:#8f2f50;font-size:19px}.index-row{margin:15px 0}.index-row>div{display:flex;align-items:baseline;gap:8px;margin-bottom:6px}.index-row b{color:#c94f76;font-size:25px}.index-row small{color:#a87a89}.reason,.risk{margin:9px 0;font-size:13px;line-height:1.65;color:#604750}.reason b,.risk b,.detail-section b{display:block;color:#9d4965}.card-footer{margin-top:14px;padding-top:12px;border-top:1px solid #f3e4e9;font-size:12px;color:#a88792}.manual-panel{max-width:820px;margin:26px auto 18px;padding:28px;border:1px solid #f0d6df;border-radius:20px;background:#fff}.manual-copy{margin-bottom:18px}.manual-empty{padding:70px}.manual-message{max-width:820px;margin:0 auto 16px}.manual-result{max-width:820px;margin:0 auto}.result-metrics{grid-template-columns:repeat(3,1fr);margin:20px 0}.review-table{margin-top:10px;border:1px solid #efdce3;border-radius:16px;overflow:hidden}.review-table :deep(th){background:#fff1f5!important;color:#9d4965!important}.detail-modal :deep(.n-card){border-radius:20px}.detail-modal small{display:block;color:#a77c8b}.detail-hero{display:grid;grid-template-columns:repeat(3,1fr);gap:10px}.detail-hero>div{padding:16px;border-radius:14px;background:#fff3f6}.detail-hero span,.detail-hero small{font-size:12px;color:#96717f}.detail-hero b{display:block;margin:5px 0;color:#a63359;font-size:22px}.detail-section{margin-top:14px}.detail-section p{line-height:1.7}.detail-actions{display:flex;justify-content:flex-end;gap:10px;margin-top:18px}@media(max-width:900px){.recommend-grid{grid-template-columns:1fr}}@media(max-width:640px){.recommend-page{padding:16px}.page-header,.section-heading{align-items:flex-start;flex-direction:column}.prediction,.result-metrics,.detail-hero{grid-template-columns:1fr}}
</style>
