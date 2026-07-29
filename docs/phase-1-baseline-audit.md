# 第一阶段：项目基线与启动核查

> 核查日期：2026-07-29。本文件只记录原项目基线，不改变任何运行时行为。

## 1. 项目与许可

- 项目定位：基于大语言模型的股票分析桌面应用，覆盖 A 股、港股、美股，提供行情、资讯、K 线、技术指标、情绪分析和 AI 分析。
- 根目录及仓库上级目录中均未发现 `AGENTS.md`，因此本次没有额外的仓库级代理约束。
- 项目采用 **GNU GPL v3**；二次分发和修改须继续遵守 GPL v3 的源代码、许可告知及修改标识要求。

## 2. 技术栈

| 层级 | 技术 |
| --- | --- |
| 桌面壳 | Wails v2 |
| 后端 | Go 1.23（`go.mod` 声明），GORM、SQLite、Resty、Chromedp、Colly |
| 前端 | Vue 3、Vite 7、Naive UI、TDesign Chat |
| 图表 | ECharts、Lightweight Charts |
| AI | OpenAI 兼容接口、Ollama、LM Studio、DeepSeek 等 |
| 辅助服务 | `ai-assistant-web`（Go HTTP 服务 + 独立 Vue/TypeScript 前端） |

主程序由 `main.go` 启动：初始化本地数据目录、机器标识、SQLite、情感分析及数据库迁移，启动 AI Assistant Web 子服务，随后运行 Wails 桌面窗口。默认数据库为 `data/stock.db`，并启用 WAL。

## 3. 目录与职责

- `main.go`、`app*.go`：Wails 生命周期、窗口、菜单以及暴露给前端的应用 API。
- `backend/data`：行情、K 线、新闻、资金流、基金、配置、AI 和搜索等数据服务。
- `backend/agent`：AI Agent、工具注册、意图识别、记忆和定时任务。
- `backend/db`、`backend/models`：SQLite/GORM 持久化和领域模型。
- `frontend/src`：主桌面端 Vue 页面、组件、路由和 K 线指标实现。
- `ai-assistant-web`：可独立访问的 AI 助手 Web 子应用。
- `build`、`scripts`：平台资源、内置股票基础数据和 Windows/macOS/Linux 构建脚本。
- `docs`：用户手册、快速开始和功能说明。

## 4. 行情与资讯数据源现状

当前实现并非单一数据源，而是按能力组合：

- **东方财富**：A 股行情、历史 K 线、资金流、板块、基金净值/估值等；部分接口有浏览器抓取兜底。
- **新浪财经**：K 线、资金流及财经直播。
- **腾讯财经**：市场指数、排行和部分实时市场数据。
- **通达信（TDX）**：K 线及行情连接能力。
- **Tushare**：需 Token 的结构化行情/基础数据能力。
- **同花顺问财（iWencai）**：自然语言选股和搜索。
- **财联社、华尔街见闻、雪球**：市场快讯、资讯、热点和舆情；雪球使用浏览器抓取。
- 内置 `build/stock_basic.json`、`stock_base_info_hk.json`、`stock_base_info_us.json` 作为股票基础数据随程序嵌入。

这些上游多为第三方公开 Web 接口或抓取页面，存在限流、Cookie、反爬、字段变化和网络可用性风险。进入“股票数量和行情准确性”阶段前，应为每个能力明确主源、备源、更新时间、复权口径和异常监控。

## 5. 启动与构建方法

### 开发启动

```bash
# 安装 Wails CLI（仓库也提供 scripts/install-wails-cli.sh）
go install github.com/wailsapp/wails/v2/cmd/wails@latest

# 安装前端依赖并启动桌面开发模式
cd frontend && npm ci && cd ..
wails dev
```

### 生产构建

```bash
cd frontend && npm ci && npm run build && cd ..
wails build
```

Linux 还需 WebKitGTK、GTK3、GCC、pkg-config 等系统依赖，具体发行版命令见 `BUILD_LINUX.md` 和 `scripts/build-linux.sh`。Windows/macOS 可使用 `scripts/build-windows.sh`、`scripts/build-macos*.sh`。

### AI Assistant Web

主程序会自动启动该子服务；也可从 `ai-assistant-web/cmd/ai-assistant-web` 单独运行。其监听地址由 `AI_ASSISTANT_WEB_ADDR` 控制。

## 6. 测试现状与本次核查结果

现有自动化测试主要是 Go 的 `*_test.go`，分布于根包、`backend/data`、`backend/agent` 和 `backend/util`。前端两个 `package.json` 都未定义测试脚本，当前只有构建校验。部分 Go 测试会访问真实第三方接口或需要浏览器/外部配置，不完全属于离线单元测试。

本次在 Linux 容器中的结果：

| 检查 | 结果 | 说明 |
| --- | --- | --- |
| `cd frontend && npm run build` | 通过 | Vite 成功生成 `frontend/dist`，满足 Go `embed` 的前置条件。 |
| `go test -p 1 ./...` | 未完成 | 超过 90 秒仍无输出，人工终止；当前测试集包含外部网络/集成测试，需后续分层并设置超时。 |
| `go build -p 1 -o /tmp/go-stock-smoke .` | 未完成 | 容器中编译超过 2 分钟仍无输出，人工终止。 |
| `wails doctor` | 环境受限 | 当前容器没有安装 Wails CLI，无法执行官方环境诊断或启动 GUI。 |

因此可以确认前端生产构建正常、源码结构具备完整启动链路，但**不能把本次容器检查表述为桌面 GUI 已成功启动**。下一步应在安装 Wails CLI 与图形依赖的 Linux 桌面环境，或 Windows/macOS CI runner 上执行 `wails doctor`、`wails dev` 和打包产物启动冒烟测试。此次仅新增核查文档，原项目代码和启动行为均保持不变。

