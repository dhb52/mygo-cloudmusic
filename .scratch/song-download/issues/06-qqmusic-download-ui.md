# qqmusic：下载入口与码率弹窗

Status: resolved
Repo: `~/Source/qqmusic`（`dhb52/qqmusic`）
Blocked by: 05

## 目标

右键菜单与"更多"菜单里出现「下载」，点击后弹窗选择可用级别（含不可用原因与目录），开始下载。

## 改动

- 入口：`pages.go:2072 trackMenu`、`main_window.go:202 songItems`。以 `state := h.host.DesktopSettings(); state.DownloadAvailable` 为门控——老宿主不宣告时完全不出现。正在下载的曲目（比对 `state.Downloads` 的 `Batch`/曲目名 + 进行中状态）显示「下载中…」并禁用。
- `home.go` 或新建 `download.go`：
  - `beginDownload(tracks []pluginapi.Track)`：生成 token（如 `"<ref>"` 或批次序号），`h.host.RequestDownloadOptions(tracks[0], token)`，记住 `pendingDownload{token, tracks}`。
  - 每帧（或 `Invalidate` 后）检查 `state.DownloadOptionsToken == pending.token` → 打开弹窗。
  - 弹窗：`ui.DialogBase(c.Key("qq-download"), &open, ...)`，逐项 `ui.Row`：级别文案（沿用 `settings_view.go:347-361` 的写法：标准（128k）/ 极高（320k）/ 无损（FLAC））＋格式与大小（`MB` 由 `pluginapi`/`megabytes` 帮助函数格式化，大小未知时不显示）；不可用项置灰并显示 `Reason`。按 (Level, Format) 去重（QQ 上 `higher`/`exhigh` 合并）。
  - 默认选中：`h.preferences.DownloadLevel`，回落到 `h.preferences.Quality`，两者都不可达则选可达的最接近级别。选中后写回 `DownloadLevel` 并 `h.savePreferences()`。
  - 目录：显示 `state.DownloadDir`；「更改…」调 `mygo.Dialog.Open(mygo.OpenDialogOptions{Directory: true, ...})`，选定后经 `ApplyDesktopPreferences`（只改 `DownloadDir`，其余字段原样带回）保存；「打开」调 `mygo.Shell.OpenPath(state.DownloadDir)`。
  - 「开始下载」：`h.host.Download(pending.tracks, level)`，关闭弹窗（宿主负责 toast 与进度）。
- 批量（多曲目）弹窗：一次选择 + 聚合标注（「无损（FLAC）· 3/5 首可用」）；不支持者由宿主记入跳过清单。**依赖 09 的决定**。

## 验收

- `go test ./...` 通过；`plugintest.NewHost` 下 `DownloadAvailable=false` 时不渲染下载入口（现有 UI 测试风格，按可访问性标签点击）。
- 手工（`LIVE=1` 环境或真实登录）：右键 → 弹窗列出可达级别、不可用项带原因 → 选择无损 → 设置窗出现任务并完成 → 文件在下载目录、有同名 `.lrc` 与封面。
- 手工：把宿主偏好 `DownloadDir` 改到一个不可写目录 → 弹窗里「开始下载」禁用并显示原因。

## Comments

qqmusic 提交 `8e55482`（入口、弹窗、设置行）与 `20bac07`（审查修复）。

审查发现的问题（都已修）：下载中的判定原先按文件名比对，而宿主给的是**清洗过的**文件名，`? : / * "`、控制字符或超长标题就会让「下载中…」失效、还能再点一次；现在按 `Ref` 比对（宿主 `player.TrackRef`，SDK 为此新增了 `DownloadItem.Ref`）。另外修掉：探测在途时再点一次会永久丢掉答案（宿主只有一个 token 槽）——现在一次只发一个探测，条目在此期间显示「正在检查音质…」并禁用；`chooseDownloadDir` 在 goroutine 里读 UI 状态（数据竞争）——改为先取 `ctx` 与目录；弹窗在切换平台后不会关闭；宿主给不出目录（空串）时「开始下载」仍是可点的；默认级别在等距时偏向更低的一档；「恢复默认」在偏好已为空时还能点。

验收：`go build ./...`、`go vet ./...`、`go test -count=1 ./...` 通过；弹窗/入口/目录/取消的关键路径有测试（`download_test.go`），其中「另一个 token 的答案不会打开弹窗」与「探测为空 → 弹窗提示且不能开始」是按审查要求补的。
