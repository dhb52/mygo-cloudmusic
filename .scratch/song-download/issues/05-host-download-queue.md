# 宿主：下载队列、任务状态与协议动作

Status: resolved
Repo: 本仓库
Blocked by: 03, 04

## 目标

把引擎接上插件：接收下载请求、串行执行、把进度与结果推进插件可见的状态里、支持取消，并把级别探测结果按 token 回推。

## 改动

- 新建 `internal/app/downloads.go`：
  - 串行队列（一次一首）；`Download(tracks []Track, level string)` 建一个批次 id，每首一个 `pluginapi.DownloadItem`，批次一个 `beginDesktopTask(protocol.TaskSongDownload)`，批次结束 `completeDesktopTask` 并 toast 汇总（成功 N / 跳过 M / 失败 K）。
  - 每首开始前：命中播放缓存（同平台 + ref + 格式 + 级别，`player.Cache` 的既有查询）→ 复制文件与元数据到下载目录而不是重新下载。
  - 队列项可取消：`CancelDownload(id)`；取消立即停止当前项并按项记录 `canceled`。
  - 状态快照：`Downloads` 保留进行中的项与最近 5 条，供 `DesktopSettings()` 填充；字段按 `pluginapi.DownloadItem`（含 `Done/Total/Path/Message`）。
- `internal/app/download_options.go` 接上状态：`RequestDownloadOptions(track, token)` 记下 token，起 goroutine 探测，完成后把 `DownloadOptions` + `DownloadOptionsToken` 推入状态并 `desktopTasksChanged()`。
- `internal/app/plugin_rpc.go`：新动作 `download-options`、`download`、`download-cancel`；曲目与级别来自 `PluginHostCall`。
- `internal/app/desktop_services.go`：`DesktopSettings()` 填 `Downloads`、`DownloadOptions`、`DownloadOptionsToken`。
- `internal/app/platform_host.go`（或 `playback_services.go`）：实现三个方法，参数校验（空曲目、空级别、未知级别、未配置的平台）返回明确失败而不是静默。
- 完成/失败的 toast 文案：成功「已下载：<歌名>」、失败「<歌名>下载失败：<原因>」、跳过「<歌名>已存在」。

## 验收

- 单测（假 adapter + `httptest`）：两首排队，第二首在第一首结束前不开始（串行）。
- 单测：批次汇总 toast 文案与计数正确（成功/跳过/失败各一例）。
- 单测：`CancelDownload` 正在下载的曲目 → 项状态 `canceled`、目录无残留、批次继续下一首。
- 单测：`RequestDownloadOptions` 用 token 关联，回推的 `DownloadOptionsToken` 等于请求 token；探测失败或没有可用音质时 token 仍回推、清单为空，插件照此提示「暂时获取不到下载音质」（契约里没有单独的说明字段，见 spec 的「探测用请求 + 状态回推 + token」）。
- 单测（`download_rpc_test.go`）：三个动作经 `pluginReverse.apply` 走通；另一个平台的曲目被拒；未知音质被拒并有提示。
- 单测：未配置平台/空曲目/未知级别的请求得到明确失败，不 panic、不进队列。
- 手工（`go tool mygo dev` + 外置 qqmusic 插件）：右键下载一首 → 设置窗任务区出现进度 → 完成后打开文件。

## Comments

宿主提交 `6cad295`、`cca4ae6`、`812945d`。

审查发现并修掉的真 bug（`downloads.go`）：运行槽只在**异步**的 `post` 里释放，于是批量的第二首起可能永远卡住（测试用同步 post 掩盖了它）。现在槽在 worker 线程上释放，`TestDownloadsAdvanceWithoutTheApplicationThread` 用"扣住 worker 投递的闭包"复现了生产时序。其余修复：取消窗口期内的取消不再报成失败；探测/入队前先整批校验；批次上限 8192；新批次开始时把在跑的批次标成 `replaced`（与 MPV 组件的既有做法一致）；关闭的批次不再留在内存；目录在入队时定下，worker 不再跨线程读设置。

验收：`internal/app/downloads_test.go`、`download_rpc_test.go`（三个动作经 `pluginReverse.apply` 走通、其它平台的曲目被拒）通过；`go test -race ./internal/...` 全绿。
