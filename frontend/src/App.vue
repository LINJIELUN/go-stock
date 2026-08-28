<script setup>
import {
  EventsEmit,
  EventsOff,
  EventsOn,
  Quit,Hide ,
  WindowFullscreen,
  WindowUnfullscreen,
  WindowSetTitle
} from '../wailsjs/runtime'
import {h, onBeforeMount, onBeforeUnmount, onMounted, ref} from "vue";
import {RouterLink, useRouter} from 'vue-router'
import {createDiscreteApi,darkTheme,lightTheme , NIcon, NText,NButton,dateZhCN,zhCN} from 'naive-ui'
import {
  AlarmOutline,
  AnalyticsOutline,
  BarChartSharp, Bonfire, BonfireOutline, CalendarOutline, DiamondOutline, EaselSharp,
  ExpandOutline, Flag,
  Flame, FlameSharp, FlaskOutline, GlobeOutline, InformationOutline,
  LogoGithub,
  ChatbubblesOutline,
  NewspaperOutline,
  NewspaperSharp, Notifications,
  PowerOutline, Pulse,
  ReorderTwoOutline,
  SettingsOutline, ServerOutline, Skull, SkullOutline, SkullSharp,
  SparklesOutline, FlashOutline, Star,
  StarOutline,
  StatsChartOutline,
  Wallet, WarningOutline, TimeOutline, SearchOutline,
} from '@vicons/ionicons5'
import {AnalyzeSentiment, GetConfig, GetEffectiveSponsorVip, GetGroupList, GetVersionInfo, IsTradingTime, IsHKTradingTime, IsUSTradingTime} from "../wailsjs/go/main/App";
import FloatingAiAssistant from "./components/FloatingAiAssistant.vue";
import FloatingAgentAssistant from "./components/FloatingAgentAssistant.vue";
import {Dragon, Fire, FirefoxBrowser, Gripfire, Robot} from "@vicons/fa";
import {Prompt, ReportAnalytics, ReportMoney, ReportSearch, TrendingUp} from "@vicons/tabler";
import {LocalFireDepartmentRound} from "@vicons/material";
import {AppsList20Regular, BoxSearch20Regular,SlideHide24Filled, CommentNote20Filled} from "@vicons/fluent";
import {FireFilled, MoneyCollectOutlined, NotificationFilled, StockOutlined} from "@vicons/antd";




const router = useRouter()
const loading = ref(true)
const loadingMsg = ref("加载数据中...")
const enableNews = ref(false)
const enableFund = ref(false)
const enableAgent = ref(false)
const enableDarkTheme = ref(darkTheme)
const content = ref('未经授权,禁止商业目的!\n\n数据来源于网络,仅供参考;投资有风险,入市需谨慎')
const isFullscreen = ref(false)
const activeKey = ref('stock')
const containerRef = ref({})
const realtimeProfit = ref(0)
const telegraph = ref([])
const groupList = ref([])
const officialStatement= ref("")
const marketStatus = ref('')
const sidebarCollapsed = ref(false)
let marketStatusTimer = null

const investmentMottos = [
  "投资有风险，入市需谨慎",
  "别人贪婪我恐惧，别人恐惧我贪婪",
  "股市有风险，投资需谨慎",
  "不要把所有鸡蛋放在一个篮子里",
  "时间是优秀企业的朋友",
  "买股票就是买公司",
  "市场短期是投票机，长期是称重机",
  "保住本金是投资的第一要务",
  "在别人恐慌时贪婪，在别人贪婪时恐慌",
  "风险来自于你不知道自己在做什么",
  "价格是你付出的，价值是你得到的",
  "投资最重要的品质是耐心",
  "机会总是留给有准备的人",
  "知行合一，方能致远",
  "顺势而为，逆势而思",
  "投资是一场马拉松，不是百米冲刺",
  "独立思考是投资成功的关键",
  "市场永远在波动，但价值终将回归",
  "控制风险比追求收益更重要",
  "学习是最好的投资",
]
const currentMotto = ref(investmentMottos[Math.floor(Math.random() * investmentMottos.length)])

function refreshMotto() {
  currentMotto.value = investmentMottos[Math.floor(Math.random() * investmentMottos.length)]
}

function updateMarketStatus() {
  Promise.all([
    IsTradingTime().catch(() => false),
    IsHKTradingTime().catch(() => false),
    IsUSTradingTime().catch(() => false)
  ]).then(([cn, hk, us]) => {
    const parts = []
    parts.push(cn ? 'A股交易中' : 'A股休市')
    parts.push(hk ? '港股交易中' : '港股休市')
    parts.push(us ? '美股交易中' : '美股休市')
    marketStatus.value = parts.join(' | ')
    WindowSetTitle("go-stock " + marketStatus.value + " " + officialStatement.value + "  「" + currentMotto.value + "」  [数据来源于网络，仅供参考；投资有风险，入市需谨慎]")
  })
}

/** 用于功能权限：仅在赞助有效期内为解密等级，否则为 0（与 EffectiveSponsorVipLevel 一致） */
const vipLevel = ref(0)
let discreteMessage = null
function getDiscreteMessage() {
  if (!discreteMessage) {
    discreteMessage = createDiscreteApi(['message'], {
      configProviderProps: {
        theme: enableDarkTheme.value ? darkTheme : lightTheme,
      },
    })
  }
  return discreteMessage.message
}
async function refreshEffectiveVip() {
  try {
    const r = await GetEffectiveSponsorVip()
    const active = !!r?.active
    const lvl = Number(r?.vipLevel ?? 0)
    vipLevel.value = active && !Number.isNaN(lvl) ? lvl : 0
  } catch (_) {
    vipLevel.value = 0
  }
}
async function handleKlineAnalysisClick() {
  await refreshEffectiveVip()
  if (vipLevel.value < 2) {
    getDiscreteMessage().warning('K线分析功能需要 VIP2 及以上赞助用户才能使用，请升级后体验')
    return
  }
  activeKey.value = 'klineAnalysis'
  router.push({ name: 'klineAnalysis' })
}

const menuOptions = ref([
  {
    label: () =>
        h(
            RouterLink,
            {
              to: {
                name: 'stock',
                query: {
                  groupName: '全部',
                  groupId: 0,
                },
                params: {},
              },
              onClick: () => {
                activeKey.value = 'stock'
              },
            },
            {default: () => '股票自选',}
        ),
    key: 'stock',
    icon: renderIcon(StarOutline),
    children: [
      {
        label: () =>
            h(
                'a',
                {
                  href: '#',
                  type: 'info',
                  onClick: () => {
                    activeKey.value = 'stock'
                    //console.log("push",item)
                    router.push({
                      name: 'stock',
                      query: {
                        groupName: '全部',
                        groupId: 0,
                      },
                    })
                    EventsEmit("changeTab", {ID: 0, name: '全部'})
                  },
                  to: {
                    name: 'stock',
                    query: {
                      groupName: '全部',
                      groupId: 0,
                    },
                  }
                },
                {default: () => '全部',}
            ),
        key: 0,
      }
    ],
  },
  {
    label: () =>
        h(
            RouterLink,
            {
              href: '#',
              to: {
                name: 'market',
                params: {}
              },
              onClick: () => {
                activeKey.value = 'market'
                EventsEmit("changeMarketTab", {ID: 0, name: '市场快讯'})
              },
            },
            {default: () => '市场行情'}
        ),
    key: 'market',
    icon: renderIcon(NewspaperOutline),
    children: [
      {
        label: () =>
            h(
                RouterLink,
                {
                  href: '#',
                  to: {
                    name: 'market',
                    query: {
                      name: "市场快讯",
                    }
                  },
                  onClick: () => {
                    activeKey.value = 'market'
                    EventsEmit("changeMarketTab", {ID: 0, name: '市场快讯'})
                  },
                },
                {default: () => '市场快讯',}
            ),
        key: 'market1',
        icon: renderIcon(NewspaperSharp),
      },
      {
        label: () =>
            h(
                RouterLink,
                {
                  href: '#',
                  to: {
                    name: 'market',
                    query: {
                      name: "全球股指",
                    },
                  },
                  onClick: () => {
                    activeKey.value = 'market'
                    EventsEmit("changeMarketTab", {ID: 0, name: '全球股指'})
                  },
                },
                {default: () => '全球股指',}
            ),
        key: 'market2',
        icon: renderIcon(BarChartSharp),
      },
      {
        label: () =>
            h(
                RouterLink,
                {
                  href: '#',
                  to: {
                    name: 'market',
                    query: {
                      name: "重大指数",
                    }
                  },
                  onClick: () => {
                    activeKey.value = 'market'
                    EventsEmit("changeMarketTab", {ID: 0, name: '重大指数'})
                  },
                },
                {default: () => '重大指数',}
            ),
        key: 'market3',
        icon: renderIcon(AnalyticsOutline),
      },
      {
        label: () =>
            h(
                RouterLink,
                {
                  href: '#',
                  to: {
                    name: 'market',
                    query: {
                      name: "行业排名",
                    }
                  },
                  onClick: () => {
                    activeKey.value = 'market'
                    EventsEmit("changeMarketTab", {ID: 0, name: '行业排名'})
                  },
                },
                {default: () => '行业排名',}
            ),
        key: 'market4',
        icon: renderIcon(Flag),
      },
      {
        label: () =>
            h(
                RouterLink,
                {
                  href: '#',
                  to: {
                    name: 'market',
                    query: {
                      name: "个股资金流向",
                    }
                  },
                  onClick: () => {
                    activeKey.value = 'market'
                    EventsEmit("changeMarketTab", {ID: 0, name: '个股资金流向'})
                  },
                },
                {default: () => '个股资金流向',}
            ),
        key: 'market5',
        icon: renderIcon(Pulse),
      },
      {
        label: () =>
            h(
                RouterLink,
                {
                  href: '#',
                  to: {
                    name: 'market',
                    query: {
                      name: "板块资金流向",
                    }
                  },
                  onClick: () => {
                    activeKey.value = 'market'
                    EventsEmit("changeMarketTab", {ID: 0, name: '板块资金流向'})
                  },
                },
                {default: () => '板块资金流向',}
            ),
        key: 'market5_1',
        icon: renderIcon(ReportMoney),
      },
      {
        label: () =>
            h(
                RouterLink,
                {
                  href: '#',
                  to: {
                    name: 'market',
                    query: {
                      name: "概念资金流向",
                    }
                  },
                  onClick: () => {
                    activeKey.value = 'market'
                    EventsEmit("changeMarketTab", {ID: 0, name: '概念资金流向'})
                  },
                },
                {default: () => '概念资金流向',}
            ),
        key: 'market5_2',
        icon: renderIcon(TrendingUp),
      },
      {
        label: () =>
            h(
                RouterLink,
                {
                  href: '#',
                  to: {
                    name: 'market',
                    query: {
                      name: "龙虎榜",
                    }
                  },
                  onClick: () => {
                    activeKey.value = 'market'
                    EventsEmit("changeMarketTab", {ID: 0, name: '龙虎榜'})
                  },
                },
                {default: () => '龙虎榜',}
            ),
        key: 'market6',
        icon: renderIcon(Dragon),
      },
      {
        label: () =>
            h(
                RouterLink,
                {
                  href: '#',
                  to: {
                    name: 'market',
                    query: {
                      name: "个股研报",
                    }
                  },
                  onClick: () => {
                    activeKey.value = 'market'
                    EventsEmit("changeMarketTab", {ID: 0, name: '个股研报'})
                  },
                },
                {default: () => '个股研报',}
            ),
        key: 'market7',
        icon: renderIcon(StockOutlined),
      },
      {
        label: () =>
            h(
                RouterLink,
                {
                  href: '#',
                  to: {
                    name: 'market',
                    query: {
                      name: "公司公告",
                    }
                  },
                  onClick: () => {
                    activeKey.value = 'market'
                    EventsEmit("changeMarketTab", {ID: 0, name: '公司公告'})
                  },
                },
                {default: () => '公司公告',}
            ),
        key: 'market8',
        icon: renderIcon(NotificationFilled),
      },
      {
        label: () =>
            h(
                RouterLink,
                {
                  href: '#',
                  to: {
                    name: 'market',
                    query: {
                      name: "行业研究",
                    }
                  },
                  onClick: () => {
                    activeKey.value = 'market'
                    EventsEmit("changeMarketTab", {ID: 0, name: '行业研究'})
                  },
                },
                {default: () => '行业研究',}
            ),
        key: 'market9',
        icon: renderIcon(ReportSearch),
      },
      {
        label: () =>
            h(
                RouterLink,
                {
                  href: '#',
                  to: {
                    name: 'market',
                    query: {
                      name: "当前热门",
                    }
                  },
                  onClick: () => {
                    activeKey.value = 'market'
                    EventsEmit("changeMarketTab", {ID: 0, name: '当前热门'})
                  },
                },
                {default: () => '当前热门',}
            ),
        key: 'market10',
        icon: renderIcon(Gripfire),
      },
      {
        label: () =>
            h(
                RouterLink,
                {
                  href: '#',
                  to: {
                    name: 'market',
                    query: {
                      name: "名站优选",
                    }
                  },
                  onClick: () => {
                    activeKey.value = 'market'
                    EventsEmit("changeMarketTab", {ID: 0, name: '名站优选'})
                  },
                },
                {default: () => '名站优选',}
            ),
        key: 'market11',
        icon: renderIcon(FirefoxBrowser),
      },
    ]
  },
  {
    label: () =>
        h(
            'div',
            {
              style: 'cursor: pointer; width: 100%;',
              onClick: () => { handleKlineAnalysisClick() },
            },
            {default: () => 'K线分析'}
        ),
    key: 'klineAnalysis',
    icon: renderIcon(StatsChartOutline),
  },
  {
    label: () =>
        h(
            RouterLink,
            {
              to: {
                name: 'fund',
                query: {
                  name: '基金自选',
                },
              },
              onClick: () => {
                activeKey.value = 'fund'
              },
            },
            {default: () => '基金自选',}
        ),
    show: enableFund.value,
    key: 'fund',
    icon: renderIcon(SparklesOutline),
    children: [
      {
        label: () =>
            h(
                RouterLink,
                {
                  to: {name: 'fund', query: {name: '基金自选'}},
                  onClick: () => {
                    activeKey.value = 'fund'
                    EventsEmit("changeFundTab", {name: '基金自选'})
                  },
                },
                {default: () => '基金自选'}
            ),
        key: 'fundFollow',
        icon: renderIcon(StarOutline),
      },
      {
        label: () =>
            h(
                RouterLink,
                {
                  to: {name: 'fund', query: {name: '基金排行'}},
                  onClick: () => {
                    activeKey.value = 'fund'
                    EventsEmit("changeFundTab", {name: '基金排行'})
                  },
                },
                {default: () => '基金排行'}
            ),
        key: 'fundRanking',
        icon: renderIcon(TrendingUp),
      },
    ]
  },
  {
    label: () =>
        h(
            RouterLink,
            {
              to: {
                name: 'agent',
                query: {
                  name:"Ai智能体",
                },
                onClick: () => {
                  activeKey.value = 'agent'
                },
              }
            },
            {default: () => 'Ai智能体'}
        ),
    key: 'agent',
    show:enableAgent.value,
    icon: renderIcon(Robot),
  },
  {
    label: () => h(
        RouterLink,
        {
          to: { name: 'aiRecommendations' },
          onClick: () => { activeKey.value = 'aiRecommendations' },
        },
        {default: () => 'AI 股票研究'}
    ),
    key: 'aiRecommendations',
    icon: renderIcon(TrendingUp),
  },
    {
      label: () =>
          h(
              RouterLink,
              {
                to: {
                  name: 'research',
                  query: {
                    name:"研究中心",
                  },
                },
                onClick: () => {
                  activeKey.value = 'research'
                  setTimeout(() => {
                    EventsEmit("changeResearchTab", {ID: 0, name: 'AI分析报告'})
                  }, 100)
                },
              },
              {default: () => '研究中心'}
          ),
      key: 'research',
      icon: renderIcon(FlaskOutline),
      children:[
          {
            label: () =>
                h(
                    RouterLink,
                    {
                      to: {
                        name: 'research',
                        query: {
                          name:"AI分析报告",
                        },
                      },
                      onClick: () => {
                        activeKey.value = 'research'
                        setTimeout(() => {
                          EventsEmit("changeResearchTab", {ID: 0, name: 'AI分析报告'})
                        }, 100)
                      },
                    },
                    {default: () => 'AI分析报告'}
                ),
            key: 'research1',
            icon: renderIcon(ReportAnalytics),
          },
        {
          label: () =>
              h(
                  RouterLink,
                  {
                    to: {
                      name: 'research',
                      query: {
                        name:"股票推荐记录",
                      },
                    },
                    onClick: () => {
                      activeKey.value = 'research'
                      setTimeout(() => {
                        EventsEmit("changeResearchTab", {ID: 1, name: '股票推荐记录'})
                      }, 100)
                    },
                  },
                  {default: () => '股票推荐记录'}
              ),
          key: 'research2',
          icon: renderIcon(Star),
        },
        {
          label: () =>
              h(
                  RouterLink,
                  {
                    to: {
                      name: 'research',
                      query: {
                        name:"异动监控",
                      },
                    },
                    onClick: () => {
                      activeKey.value = 'research'
                      setTimeout(() => {
                        EventsEmit("changeResearchTab", {ID: 2, name: '异动监控'})
                      }, 100)
                    },
                  },
                  {default: () => '异动监控'}
              ),
          key: 'stockChanges',
          icon: renderIcon(TrendingUp),
        },
        {
          label: () =>
              h(
                  RouterLink,
                  {
                    to: {
                      name: 'research',
                      query: {
                        name:"涨停梯队",
                      },
                    },
                    onClick: () => {
                      activeKey.value = 'research'
                      setTimeout(() => {
                        EventsEmit("changeResearchTab", {ID: 9, name: '涨停梯队'})
                      }, 100)
                    },
                  },
                  {default: () => '涨停梯队'}
              ),
          key: 'uplimitLadder',
          icon: renderIcon(LocalFireDepartmentRound),
        },
        {
          label: () =>
              h(
                  RouterLink,
                  {
                    to: {
                      name: 'research',
                      query: {
                        name:"提示词模板",
                      },
                    },
                    onClick: () => {
                      activeKey.value = 'research'
                      setTimeout(() => {
                        EventsEmit("changeResearchTab", {ID: 3, name: '提示词模板'})
                      }, 100)
                    },
                  },
                  {default: () => '提示词模板'}
              ),
          key: 'research3',
          icon: renderIcon(Prompt),
        },
        {
          label: () =>
              h(
                  RouterLink,
                  {
                    to: {
                      name: 'research',
                      query: {
                        name:"提示词广场",
                      },
                    },
                    onClick: () => {
                      activeKey.value = 'research'
                      setTimeout(() => {
                        EventsEmit("changeResearchTab", {ID: 10, name: '提示词广场'})
                      }, 100)
                    },
                  },
                  {default: () => '提示词广场'}
              ),
          key: 'promptPlaza',
          icon: renderIcon(GlobeOutline),
        },
        {
          label: () =>
              h(
                  RouterLink,
                  {
                    to: {
                      name: 'research',
                      query: {
                        name:"问答广场",
                      },
                    },
                    onClick: () => {
                      activeKey.value = 'research'
                      setTimeout(() => {
                        EventsEmit("changeResearchTab", {ID: 11, name: '问答广场'})
                      }, 100)
                    },
                  },
                  {default: () => '问答广场'}
              ),
          key: 'promptQa',
          icon: renderIcon(ChatbubblesOutline),
        },
        {
          label: () =>
              h(
                  RouterLink,
                  {
                    to: {
                      name: 'research',
                      query: {
                        name:"形态选股",
                      },
                    },
                    onClick: () => {
                      activeKey.value = 'research'
                      setTimeout(() => {
                        EventsEmit("changeResearchTab", {ID: 3, name: '形态选股'})
                      }, 100)
                    },
                  },
                  {default: () => '形态选股'}
              ),
          key: 'research4',
          icon: renderIcon(SearchOutline),
        },
        {
          label: () =>
              h(
                  RouterLink,
                  {
                    to: {
                      name: 'research',
                      query: {
                        name:"指标选股",
                      },
                    },
                    onClick: () => {
                      activeKey.value = 'research'
                      setTimeout(() => {
                        EventsEmit("changeResearchTab", {ID: 0, name: '指标选股'})
                      }, 100)
                    },
                  },
                  {default: () => '指标选股'}
              ),
          key: 'research_select_stock',
          icon: renderIcon(BoxSearch20Regular),
        },
        {
          label: () =>
              h(
                  RouterLink,
                  {
                    to: {
                      name: 'research',
                      query: {
                        name:"定时任务",
                      },
                    },
                    onClick: () => {
                      activeKey.value = 'research'
                      setTimeout(() => {
                        EventsEmit("changeResearchTab", {ID: 5, name: '定时任务'})
                      }, 100)
                    },
                  },
                  {default: () => '定时任务'}
              ),
          key: 'research5',
          icon: renderIcon(TimeOutline),
        },
        {
          label: () =>
              h(
                  RouterLink,
                  {
                    to: {
                      name: 'research',
                      query: {
                        name:"交易日志",
                      },
                    },
                    onClick: () => {
                      activeKey.value = 'research'
                      setTimeout(() => {
                        EventsEmit("changeResearchTab", {ID: 6, name: '交易日志'})
                      }, 100)
                    },
                  },
                  {default: () => '交易日志(beta)'}
              ),
          key: 'research6',
          icon: renderIcon(MoneyCollectOutlined),
        },
        {
          label: () =>
              h(
                  RouterLink,
                  {
                    to: {
                      name: 'research',
                      query: {
                        name:"每日操作计划",
                      },
                    },
                    onClick: () => {
                      activeKey.value = 'research'
                      setTimeout(() => {
                        EventsEmit("changeResearchTab", {ID: 7, name: '每日操作计划'})
                      }, 100)
                    },
                  },
                  {default: () => '每日操作计划'}
              ),
          key: 'dailyOperationPlan',
          icon: renderIcon(CalendarOutline),
        },
        {
          label: () =>
              h(
                  RouterLink,
                  {
                    to: {
                      name: 'research',
                    },
                    onClick: () => {
                      activeKey.value = 'research'
                      setTimeout(() => {
                        EventsEmit("changeResearchTab", {ID: 7, name: 'MCP服务'})
                      }, 100)
                    },
                  },
                  {default: () => 'MCP服务'}
              ),
          key: 'mcpServers',
          icon: renderIcon(ServerOutline),
        },
        {
          label: () =>
              h(
                  RouterLink,
                  {
                    to: {
                      name: 'research',
                    },
                    onClick: () => {
                      activeKey.value = 'research'
                      setTimeout(() => {
                        EventsEmit("changeResearchTab", {ID: 8, name: '技能管理'})
                      }, 100)
                    },
                  },
                  {default: () => '技能管理'}
              ),
          key: 'skills',
          icon: renderIcon(FlashOutline),
          show: false,
        },
      ],
    },
  {
    label: '设置',
    key: 'settings',
    icon: renderIcon(SettingsOutline),
    children: [
      {
        label: () =>
            h(
                RouterLink,
                {
                  to: {
                    name: 'settings',
                    query: {
                      name:"设置",
                    },
                  },
                  onClick: () => {
                    activeKey.value = 'settings'
                  },
                },
                {default: () => '基础设置'}
            ),
        key: 'settings',
        icon: renderIcon(SettingsOutline),
      },
      {
        label: () =>
            h(
                RouterLink,
                {
                  to: {
                    name: 'aiConfigs',
                    query: {
                      name:"AI模型服务",
                    },
                  },
                  onClick: () => {
                    activeKey.value = 'aiConfigs'
                  },
                },
                {default: () => 'AI模型服务'}
            ),
        key: 'aiConfigs',
        icon: renderIcon(SparklesOutline),
      },
    ],
  },
  {
    label: () =>
        h(
            RouterLink,
            {
              to: {
                name: 'about',
                query: {
                  name:"关于",
                }
              },
              onClick: () => {
                activeKey.value = 'about'
              },
            },
            {default: () => '关于'}
        ),
    key: 'about',
    icon: renderIcon(LogoGithub),
    show: true,
  },
  {
    show:false,
    label: () => h("a", {
      href: '#',
      onClick: toggleFullscreen,
      title: '全屏 Ctrl+F 退出全屏 Esc',
    }, {default: () => isFullscreen.value ? '取消全屏' : '全屏'}),
    key: 'full',
    icon: renderIcon(ExpandOutline),
  },
  // {
  //   label: ()=> h("a", {
  //     href: 'javascript:void(0)',
  //     style: 'cursor: move;',
  //     onClick: toggleStartMoveWindow,
  //   }, { default: () => '移动' }),
  //   key: 'move',
  //   icon: renderIcon(MoveOutline),
  // },
  {
    label: () => h("a", {
      href: '#',
      onClick: Hide,
    }, {default: () => '隐藏至托盘区'}),
    key: 'hide',
    icon: renderIcon(SlideHide24Filled),
  },
  {
    label: () => h("a", {
      href: '#',
      onClick: Quit,
    }, {default: () => '退出程序'}),
    key: 'exit',
    icon: renderIcon(PowerOutline),
  },
])

// 将高频业务入口与低频系统操作分区，避免所有功能挤在同一条底部菜单中。
// 菜单项本身仍复用原来的路由与事件处理，确保改版不改变既有功能行为。
const businessMenuOptions = menuOptions.value.filter((item) =>
    ['stock', 'market', 'klineAnalysis', 'fund', 'research'].includes(item.key)
)
const intelligentMenuOptions = menuOptions.value.filter((item) => ['agent', 'aiShadowReport'].includes(item.key))
const systemMenuOptions = menuOptions.value.filter((item) => ['settings', 'about'].includes(item.key))
const windowMenuOptions = menuOptions.value.filter((item) => ['full', 'hide', 'exit'].includes(item.key))

// 重建"股票自选"菜单的分组子项（保留"全部"，用最新分组列表替换其余子项）
function refreshStockGroupMenu() {
  GetGroupList().then(result => {
    groupList.value = result
    menuOptions.value.forEach((item) => {
      if (item.key === 'stock') {
        const allItem = item.children.find(c => c.key === 0)
        item.children = allItem ? [allItem] : []
        item.children.push(...groupList.value.map(g => {
          return {
            label: () =>
                h(
                    'a',
                    {
                      href: '#',
                      type: 'info',
                      onClick: () => {
                        router.push({
                          name: 'stock',
                          query: {
                            groupName: g.name,
                            groupId: g.ID,
                          },
                        })
                        setTimeout(() => {
                          EventsEmit("changeTab", g)
                        }, 100)
                      },
                      to: {
                        name: 'stock',
                        query: {
                          groupName: g.name,
                          groupId: g.ID,
                        },
                      }
                    },
                    {default: () => g.name,}
                ),
            key: g.ID,
          }
        }))
      }
    })
  }).catch(err => {
    console.error("refreshStockGroupMenu error:", err)
  })
}

function renderIcon(icon) {
  return () => h(NIcon, null, {default: () => h(icon)})
}

function toggleFullscreen(e) {
  activeKey.value = 'full'
  //console.log(e)
  if (isFullscreen.value) {
    WindowUnfullscreen()
    //e.target.innerHTML = '全屏'
  } else {
    WindowFullscreen()
    // e.target.innerHTML = '取消全屏'
  }
  isFullscreen.value = !isFullscreen.value
}

// const drag = ref(false)
// const lastPos= ref({x:0,y:0})
// function toggleStartMoveWindow(e) {
//   drag.value=!drag.value
//   lastPos.value={x:e.clientX,y:e.clientY}
// }
// function dragstart(e) {
//   if (drag.value) {
//     let x=e.clientX-lastPos.value.x
//     let y=e.clientY-lastPos.value.y
//     WindowGetPosition().then((pos) => {
//       WindowSetPosition(pos.x+x,pos.y+y)
//     })
//   }
// }
// window.addEventListener('mousemove', dragstart)

EventsOn("realtime_profit", (data) => {
  realtimeProfit.value = data
})
EventsOn("telegraph", (data) => {
  telegraph.value = data
})

EventsOn("loadingMsg", (data) => {
  if(data==="done"){
    loadingMsg.value = "加载完成..."
    EventsEmit("loadingDone", "app")
    loading.value  = false
  }else{
    loading.value  = true
    loadingMsg.value = data
  }
})

setTimeout(() => {
  if (loading.value) {
    loading.value = false
    loadingMsg.value = "加载完成..."
    EventsEmit("loadingDone", "app")
  }
}, 8000)

onBeforeUnmount(() => {
  if (marketStatusTimer) {
    clearInterval(marketStatusTimer)
    marketStatusTimer = null
  }
  EventsOff("realtime_profit")
  EventsOff("loadingMsg")
  EventsOff("telegraph")
  EventsOff("newsPush")
  EventsOff("groupListChanged")
})

window.onerror = function (msg, source, lineno, colno, error) {
  // 将错误信息发送给后端
  EventsEmit("frontendError", {
    page: "App.vue",
    message: msg,
    source: source,
    lineno: lineno,
    colno: colno,
    error: error ? error.stack : null,
  });
  return true;
};

onBeforeMount(() => {
  GetVersionInfo().then(result => {
    if(result.officialStatement){
      content.value = result.officialStatement+"\n\n"+content.value
    }
    officialStatement.value = result.officialStatement || ""
    updateMarketStatus()
  }).catch(err => {
    console.error("GetVersionInfo error:", err)
  })

  refreshStockGroupMenu()
  // 监听分组变化（新增/改名/删除），实时刷新菜单栏
  EventsOn("groupListChanged", () => {
    refreshStockGroupMenu()
  })


  GetConfig().then((res) => {
    enableFund.value = res.enableFund
    enableAgent.value = res.enableAgent

    menuOptions.value.filter((item) => {
      if (item.key === 'fund') {
        item.show = res.enableFund
      }
      if (item.key === 'agent') {
        item.show = res.enableAgent
      }
    })

    if (res.darkTheme) {
      enableDarkTheme.value = darkTheme
    } else {
      enableDarkTheme.value = null
    }
  }).catch(err => {
    console.error("GetConfig error:", err)
  })
})

onMounted(() => {
  updateMarketStatus()
  marketStatusTimer = setInterval(() => {
    refreshMotto()
    updateMarketStatus()
  }, 60000)
  GetConfig().then((res) => {
    if (res.enableNews) {
      enableNews.value = true
    }
    enableFund.value = res.enableFund
    enableAgent.value = res.enableAgent
    const {notification } =createDiscreteApi(["notification"], {
      configProviderProps: {
        theme: enableDarkTheme.value ? darkTheme : lightTheme ,
        max: 3,
      },
    })
    EventsOn("newsPush", (data) => {
      //console.log(data)
      if(data.isRed){
        notification.create({
          //type:"error",
         // avatar: () => h(NIcon,{component:Notifications,color:"red"}),
          title: data.time,
          content: () => h('div',{type:"error",style:{
              "text-align":"left",
              "font-size":"14px",
              "color":"#f67979"
            }}, { default: () => data.content }),
          meta: () => h(NText,{type:"warning"}, { default: () => data.source}),
          duration:1000*40,
        })
      }else{
         notification.create({
          //type:"info",
          //avatar: () => h(NIcon,{component:Notifications}),
          title: data.time,
          content: () => h('div',{type:"info",style:{
            "text-align":"left",
              "font-size":"14px",
              "color": data.source==="go-stock"?"#F98C24":"#549EC8"
            }}, { default: () => data.content }),
          meta: () => h(NText,{type:"warning"}, { default: () => data.source}),
          duration:1000*30 ,
        })
      }
    })
  }).catch(err => {
    console.error("GetConfig(onMounted) error:", err)
  })
})
</script>
<template>
  <n-config-provider ref="containerRef" :theme="enableDarkTheme" :locale="zhCN" :date-locale="dateZhCN">
    <n-message-provider>
      <n-notification-provider>
        <n-modal-provider>
          <n-dialog-provider>
            <n-watermark
                :content="''"
                cross
                selectable
                :font-size="16"
                :line-height="16"
                :width="500"
                :height="400"
                :x-offset="50"
                :y-offset="150"
                :rotate="-15"
            >
<!--              <FloatingAiAssistant />-->
              <FloatingAgentAssistant />
              <div class="app-shell" :class="{ 'is-dark': !!enableDarkTheme }">
                <aside class="app-sidebar" :class="{ 'is-collapsed': sidebarCollapsed }" aria-label="主功能导航">
                  <div class="sidebar-brand">
                    <div class="brand-mark">杰</div>
                    <div v-show="!sidebarCollapsed" class="brand-copy">
                      <strong>杰伦股市分析</strong>
                      <span>智能投资研究工作台</span>
                    </div>
                    <n-button class="collapse-button" quaternary circle size="small"
                              :title="sidebarCollapsed ? '展开功能导航' : '收起功能导航'"
                              @click="sidebarCollapsed = !sidebarCollapsed">
                      <template #icon><n-icon><ReorderTwoOutline /></n-icon></template>
                    </n-button>
                  </div>

                  <n-scrollbar class="sidebar-scroll">
                    <nav class="sidebar-nav">
                      <div class="nav-section">
                        <div v-show="!sidebarCollapsed" class="nav-section-title">行情与交易</div>
                        <n-menu
                            v-model:value="activeKey"
                            :options="businessMenuOptions"
                            :collapsed="sidebarCollapsed"
                            :collapsed-width="64"
                            :collapsed-icon-size="22"
                            :indent="18"
                        />
                      </div>
                      <div class="nav-section">
                        <div v-show="!sidebarCollapsed" class="nav-section-title">智能助手</div>
                        <n-menu
                            v-model:value="activeKey"
                            :options="intelligentMenuOptions"
                            :collapsed="sidebarCollapsed"
                            :collapsed-width="64"
                            :collapsed-icon-size="22"
                            :indent="18"
                        />
                      </div>
                    </nav>
                  </n-scrollbar>

                  <div class="sidebar-footer">
                    <div v-show="!sidebarCollapsed" class="nav-section-title">系统</div>
                    <n-menu
                        v-model:value="activeKey"
                        :options="systemMenuOptions"
                        :collapsed="sidebarCollapsed"
                        :collapsed-width="64"
                        :collapsed-icon-size="22"
                        :indent="18"
                    />
                    <n-divider class="sidebar-divider" />
                    <n-menu
                        v-model:value="activeKey"
                        :options="windowMenuOptions"
                        :collapsed="sidebarCollapsed"
                        :collapsed-width="64"
                        :collapsed-icon-size="22"
                        :indent="18"
                    />
                  </div>
                </aside>

                <main class="app-workspace">
                  <div class="workspace-status" aria-live="polite">
                    <span class="status-dot"></span>
                    <span>{{ marketStatus || '正在获取市场状态' }}</span>
                  </div>
                  <div class="workspace-content">
                    <n-spin :show="loading">
                      <template #description>
                        {{ loadingMsg }}
                      </template>
                      <n-marquee class="workspace-marquee" :speed="100"
                                 v-if="(telegraph.length>0)&&(enableNews)">
                        <n-tag type="warning" v-for="item in telegraph" style="margin-right: 10px">
                          {{ item }}
                        </n-tag>
                      </n-marquee>
                      <n-scrollbar class="workspace-scroll">
                        <n-skeleton v-if="loading" height="calc(100vh)" />
                        <RouterView/>
                      </n-scrollbar>
                    </n-spin>
                  </div>
                </main>
              </div>
            </n-watermark>
          </n-dialog-provider>
        </n-modal-provider>
      </n-notification-provider>
    </n-message-provider>
  </n-config-provider>
</template>
<style scoped>
.app-shell {
  --shell-surface: #fff;
  --shell-background: #f4f6f8;
  --shell-border: #e5e7eb;
  display: flex;
  width: 100%;
  height: 100vh;
  overflow: hidden;
  background: var(--shell-background);
  text-align: left;
}

.app-shell.is-dark {
  --shell-surface: #18181c;
  --shell-background: #101014;
  --shell-border: rgba(255, 255, 255, 0.09);
  color: rgba(255, 255, 255, 0.82);
  background: var(--shell-background);
}

.app-sidebar {
  position: relative;
  z-index: 20;
  display: flex;
  flex: 0 0 232px;
  flex-direction: column;
  min-width: 0;
  height: 100vh;
  box-sizing: border-box;
  border-right: 1px solid var(--shell-border);
  background: var(--shell-surface);
  box-shadow: 6px 0 24px rgba(15, 23, 42, 0.04);
  --wails-draggable: no-drag;
  transition: flex-basis 0.2s ease;
}

.app-sidebar.is-collapsed {
  flex-basis: 64px;
}

.is-dark .app-sidebar,
.is-dark .sidebar-footer {
  border-color: var(--shell-border);
  background: var(--shell-surface);
}

.sidebar-brand {
  display: flex;
  align-items: center;
  gap: 12px;
  min-height: 72px;
  padding: 0 18px;
  border-bottom: 1px solid var(--n-border-color, #eef0f2);
  --wails-draggable: drag;
}

.collapse-button {
  margin-left: auto;
  --wails-draggable: no-drag;
}

.is-collapsed .sidebar-brand {
  justify-content: center;
  padding: 0;
}

.is-collapsed .brand-mark {
  display: none;
}

.is-collapsed .collapse-button {
  margin-left: 0;
}

.brand-mark {
  display: grid;
  width: 36px;
  height: 36px;
  place-items: center;
  border-radius: 11px;
  background: linear-gradient(145deg, #18a058, #0f7a43);
  color: #fff;
  font-size: 19px;
  font-weight: 800;
  box-shadow: 0 6px 16px rgba(24, 160, 88, 0.24);
}

.brand-copy {
  display: flex;
  flex-direction: column;
  min-width: 0;
  line-height: 1.25;
}

.brand-copy strong {
  font-size: 17px;
  letter-spacing: 0.2px;
}

.brand-copy span {
  margin-top: 4px;
  color: var(--n-text-color-3, #8b949e);
  font-size: 11px;
}

.sidebar-scroll {
  min-height: 0;
  flex: 1;
}

.sidebar-nav {
  padding: 12px 10px;
}

.is-collapsed .sidebar-nav,
.is-collapsed .sidebar-footer {
  padding-right: 0;
  padding-left: 0;
}

.nav-section + .nav-section {
  margin-top: 14px;
  padding-top: 12px;
  border-top: 1px solid var(--n-border-color, #eef0f2);
}

.nav-section-title {
  padding: 0 10px 7px;
  color: var(--n-text-color-3, #8b949e);
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.12em;
}

.sidebar-footer {
  padding: 10px;
  border-top: 1px solid var(--n-border-color, #eef0f2);
  background: var(--n-card-color, #fff);
}

.sidebar-divider {
  margin: 6px 0;
}

.app-sidebar :deep(.n-menu-item-content) {
  border-radius: 8px;
}

.app-sidebar :deep(.n-menu-item-content-header) {
  font-size: 14px;
}

.app-workspace {
  position: relative;
  min-width: 0;
  flex: 1;
  height: 100vh;
  overflow: hidden;
}

.workspace-status {
  position: absolute;
  top: 12px;
  right: 18px;
  z-index: 18;
  display: flex;
  align-items: center;
  gap: 7px;
  max-width: min(520px, 55%);
  padding: 6px 11px;
  border: 1px solid var(--n-border-color, rgba(0, 0, 0, 0.08));
  border-radius: 999px;
  background: color-mix(in srgb, var(--n-card-color, #fff) 88%, transparent);
  color: var(--n-text-color-2, #59636e);
  font-size: 12px;
  white-space: nowrap;
  box-shadow: 0 4px 16px rgba(15, 23, 42, 0.06);
  backdrop-filter: blur(10px);
  pointer-events: none;
}

.status-dot {
  width: 7px;
  height: 7px;
  flex: 0 0 auto;
  border-radius: 50%;
  background: #18a058;
  box-shadow: 0 0 0 3px rgba(24, 160, 88, 0.14);
}

.workspace-content,
.workspace-content :deep(.n-spin-container) {
  height: 100%;
}

.workspace-content :deep(.n-spin-content) {
  display: flex;
  min-height: 0;
  height: 100%;
  flex-direction: column;
}

.workspace-marquee {
  position: relative;
  z-index: 19;
  width: 100%;
  flex: 0 0 auto;
}

.workspace-scroll {
  min-height: 0;
  flex: 1 1 auto;
}

@media (max-width: 900px) {
  .app-sidebar {
    flex-basis: 196px;
  }

  .brand-copy span {
    display: none;
  }

  .workspace-status {
    display: none;
  }
}
</style>
