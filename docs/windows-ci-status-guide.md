# Windows CI 状态与下载指南

## 非默认分支没有 `Run workflow`

`Run workflow` 来自 GitHub Actions 的手动触发入口。工作流只存在于非默认分支时，该按钮可能不显示，不需要为了显示按钮而修改默认分支。

Windows 构建同时监听 Pull Request。只要分支已经创建 PR，提交或更新工作流文件后，GitHub 会自动启动构建。出现以下信息即表示已经开始：

- 左侧选中 `Windows portable smoke build`；
- 中间出现一条 workflow run；
- 记录显示当前分支；
- 状态为黄色圆点或 `In progress`。

此时点击该运行记录查看步骤并等待，不要重复创建 PR、修改默认分支或重复提交相同文件。

## 构建结束后

### 绿色勾

1. 点击已完成的运行记录。
2. 如果页面中间显示 `Set up job`、`Checkout`、`Build portable executable` 等日志步骤，说明当前位于 job 日志页；此页底部不会显示 artifact 下载区。
3. 点击页面左上角带小房子图标的 `Summary`，返回本次运行的摘要页。
4. 在 Summary 页面向下滚动到最底部，找到 `Artifacts`。
5. 下载名称以 `go-stock-windows-amd64-` 开头的 ZIP。
6. 完整解压 ZIP 后再运行 `.exe`，不要在压缩包内直接运行。

如果 `Upload portable executable` 步骤是绿色勾，说明上传步骤已成功；通常只需要返回 Summary 页面即可看到下载入口。页面顶部关于 Node.js 版本弃用的黄色 `Annotations` 是警告而非本次构建失败，不会阻止已经成功的 artifact 上传。

### 红色叉

1. 点击失败的运行记录。
2. 打开 `build-windows-portable` job。
3. 展开第一个带红叉的步骤。
4. 保存包含步骤名称和完整错误信息的截图。

红叉表示没有生成可用的本次构建产物，不能用旧 artifact 或源码 ZIP 替代。

### 长时间为黄色

点击运行记录确认它是正在执行还是排队。如果某个步骤长时间没有新日志，记录步骤名称和最后可见日志；不要连续触发多个相同构建。

## 验收边界

Actions 绿色只证明 Windows 可执行文件成功编译。下载后仍需在 Windows 上验证首次启动、退出、再次启动、股票搜索、自选持久化和 K 线。当前 ETF 基金池仍是单独的后续数据功能，不能因便携版构建成功就视为 ETF 需求已经完成。
