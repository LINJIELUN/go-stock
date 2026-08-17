# GitHub 行情项目候选快照

抓取时间：2026-08-17（UTC）。机器可读快照见 [`github-marketdata-candidates.json`](./github-marketdata-candidates.json)。

## 先纠正筛选前提

GitHub Star、Fork 和近期提交只能辅助判断社区关注度与维护状态，不能证明行情准确、获得交易所授权、允许桌面展示或能稳定提供完整收盘数据。大部分候选是“访问其他网站或协议的客户端库”，不是对数据质量和授权承担 SLA 的商业供应商。

因此本快照只用于生成技术候选，不会自动填充 `ProviderDeclaration.DisplayRightsVerified`，也不会绕过供应商准入门槛。

## 本轮候选结论

| 项目 | 2026-08-17 GitHub 状态 | 当前判断 |
| --- | --- | --- |
| [AKShare](https://github.com/akfamily/akshare) | 22,072 Stars；MIT；未归档；最近推送 2026-08-13 | 适合验证字段与数据源覆盖。README 明确感谢东方财富、新浪等上游网站，因此 MIT 只覆盖库代码，不能推导上游数据展示授权或 SLA。|
| [TuShare](https://github.com/waditu/tushare) | 15,352 Stars；BSD-3-Clause；未归档；最近推送 2024-03-13 | GitHub 仓库适合参考调用方式；实际 Pro 数据权限、积分、频次和价格必须以 TuShare 官方权限中心为准，仓库 Star 不能替代套餐核实。|
| [efinance](https://github.com/Micro-sheep/efinance) | 3,939 Stars；MIT；未归档；最近推送 2026-07-17 | README 明确“仅供学习交流、不得用于商业用途”，不能作为默认生产行情源。即使本软件暂不收费，也仍需单独核实桌面展示和上游条款。|
| [mootdx](https://github.com/mootdx/mootdx) | 2,207 Stars；MIT；未归档；最近推送 2024-07-16 | README 明确“只作学习交流、不得用于任何商业目的”。可用于协议研究和本地验证，不能据此宣称获得通达信数据授权。|
| [pytdx](https://github.com/rainx/pytdx) | 1,553 Stars；GitHub API 未识别 SPDX；已归档；最近推送 2020-04-15 | 已归档且 README 提醒不得商业使用，不应作为新生产适配器的首选。|

## 下一步选择

1. **AKShare**：只作为字段发现与交叉核对工具，不直接嵌入第一版 Go/Wails 生产运行时。
2. **TuShare Pro**：进入官方套餐、北交所/ETF覆盖、频次和展示权核实；在核实前不创建“合格”声明。
3. **efinance / mootdx / pytdx**：保留研究候选身份，不作为生产主源。

候选优先级现改为收盘数据完整性、复权口径、交易日覆盖、沪深北/ETF覆盖和展示权。不能因为某个项目 Star 高，就把未经复核的网页抓取结果用于 AI 推荐基准价或七交易日复盘。
