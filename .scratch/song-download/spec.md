# 歌曲下载

设计已定（两轮 grill），待开工。涉及三个仓库：`waygu-music-plugin/sdk`、宿主 `dhb52/mygo-cloudmusic`、平台插件 `dhb52/qqmusic`。

## 目标

用户在歌曲上发起下载，下载前选择**可用的音质级别**，成品写入**可配置的下载目录**。宿主执行下载与落盘，平台插件提供入口、码率弹窗与设置行。

范围外：标签写入（mp3 ID3 / flac Vorbis comment）、断点续传、限速、专辑/歌单的自动整理目录结构、"下载整张专辑/歌单"之外的批量交互（见 09）。

## 决策

1. **分层**：SDK 加契约与偏好位；宿主实现探测、下载引擎、队列与任务状态；插件加入口、弹窗与设置行。
2. **谁下载**：宿主。插件只提交曲目与级别。
3. **级别呈现**：以级别为选项，展示探测到的真实格式与大小（如「无损 · FLAC · 32.4 MB」）；文案沿用平台自己的写法（QQ：标准（128k）/ 极高（320k）/ 无损（FLAC））。
4. **可用性判定**：宿主对候选级别逐个调平台 `Stream()`，读回 `Level/Type/Size`；`Level` 低于请求即视为不可用，并给原因（需要登录 / 需要会员 / 该曲目无此音质）。探测结果按（平台, 曲目 ref）在宿主内存缓存 5 分钟。QQ 上 `higher` 与 `exhigh` 都落到 320k，按 (Level, Format) 去重合并为一项。
   - 本条已被 `.scratch/download-level-fallback/spec.md` 取代：不再探测（`RequestDownloadOptions`、`DownloadOptions`、`DownloadOptionsToken`、`download_options.go` 全部删除），插件按自己的级别表列出级别，宿主按平台实际服务的级别落盘；`higher`/`exhigh` 仍都请求 `M800`，但只列 `higher`，记录里写实际服务的 `exhigh`。
5. **下载目录**：宿主级偏好 `DesktopPreferences.DownloadDir`（新位 `PrefDownloadDir`）；空值 = 系统下载目录；不存在则 `MkdirAll(0o755)`；`~/` 展开；点击"开始下载"前预检可写，不可写则报错并禁用开始。
6. **命名**：平铺于下载目录，`歌手 - 歌名.ext`（多歌手用 `Track.ArtistNames()`），非法字符与控制字符替换为 `_`，曲名按 UTF-8 边界截到 ~120 字节；同名同大小文件跳过并提示"已存在"，否则追加 ` (2)`。
7. **附带文件**：同名 `.lrc`（有译文时合并为同时间戳两行，丢弃 `.yrc`）；同名封面图（按内容嗅探 jpg/png，音频重名 ` (2)` 时封面跟同后缀，已存在则跳过）；不写音频标签。
8. **任务模型**：串行队列一次一首；每首一个下载任务、每批一个桌面任务（kind `song-download`）；设置窗「下载」区显示进行中的一首（进度 + 取消）与最近 5 条（状态/原因 + 打开文件）；每首完成或失败各一条 toast，批量结束补一条汇总。菜单里正在下载的那首显示「下载中…」并禁用。
9. **完成判定**：不信任 `Stream.Size`。响应有 `Content-Length` 且与实际字节不符 → 失败；无 `Content-Length`（或无 `Size`）→ 以 EOF 完成。这条严格策略只用于下载。
10. **重试**：失败整段重下，最多 3 次尝试（延迟 1s、3s）；URL 过期交给既有的 `MediaSource` 续签（仅 401/403 触发）。不做 Range 续传。
11. **与缓存的关系**：命中播放缓存（同平台 + ref + 格式 + 级别）时复制一份到下载目录，缓存副本保留。
12. **记忆**：插件在自己的偏好里存 `downloadLevel`，弹窗默认选中它；首次回落到平台音质设置，若不可达则选可达的最接近级别。
13. **能力宣告**：宿主在 `DesktopSettings` 里宣告 `DownloadAvailable`（走握手 `Needs` 会让老宿主拒绝启动插件；新偏好位在老宿主上会让整个补丁失效）。插件只在 `DownloadAvailable` 为真时渲染下载入口、弹窗与设置行。

## 契约（SDK v0.2.0，协议仍为 v1）

新增 `pluginapi/download.go`：

```go
// DownloadOption is one quality a track may be downloaded at.
type DownloadOption struct {
    Level     string // standard/higher/exhigh/lossless/hires
    Format    string // mp3 或 flac，探测所得
    Size      int64  // 字节；0 表示未知
    Available bool
    Reason    string // 不可用的原因，展示给用户
}

// DownloadItem is one track's download in the settings window.
type DownloadItem struct {
    ID      uint64
    Batch   uint64
    Name    string // 歌手 - 歌名，仅供展示（宿主清洗过文件名）
    Ref     string // 曲目在平台上的引用：插件用它把自己发起的那首对上号
    Level   string
    Phase   protocol.TaskPhase // running/succeeded/failed/canceled
    Done    int64
    Total   int64 // 0 表示未知
    Path    string // 成功后的音频文件路径
    Message string // 失败原因，或"已存在"
}
```

`Phase` 用 `protocol.TaskPhase`（与 `DesktopTask.Phase` 一致），线上一侧本来就是它。`Name` 是给用户看的（宿主按文件系统规则清洗过），插件要判断"这首在不在下载中"必须用 `Ref`。

- `DesktopPreferences` 加 `DownloadDir string`；`protocol.DesktopPreferences` 同步，位 `PrefDownloadDir = 1 << 12`，`Valid`/`Changes`/`ApplyTo`/`allDesktopPreferences` 一并更新；`bridge.Wire*/Local*` 两处映射补齐。
- `DesktopSettings`（`pluginapi` 与 `protocol` 两份）加 `DownloadAvailable bool`、`DownloadDir string`（宿主解析后的**生效**目录，供展示）、`Downloads []DownloadItem`、`DownloadOptions []DownloadOption`、`DownloadOptionsToken string`。
  - `DownloadOptions`/`DownloadOptionsToken` 已被 `.scratch/download-level-fallback/spec.md` 删除：级别由插件自己列出，`DesktopSettings` 只宣告下载能力、目录与在下的条目。
- `desktop.DesktopServices` 加三个**即发即忘**的方法（插件 UI 线程不能等网络）：

```go
RequestDownloadOptions(track Track, token string)
Download(tracks []Track, level string)
CancelDownload(id uint64)
```

  `RequestDownloadOptions` 已被 `.scratch/download-level-fallback/spec.md` 删除，只剩 `Download` 与 `CancelDownload`。
  探测结果通过 `DesktopSettings.DownloadOptions` + `DownloadOptionsToken` 回到插件（与 `DecoderStatus.Done/Total` 同一套状态推送方式）。**列表为空而 token 对得上**就是这次探测没有拿到任何音质（平台不可用、被取消或全部被拒），插件照此提示；契约里没有单独的说明字段。
  - 这一段随探测一并删除（`.scratch/download-level-fallback/spec.md`）：弹窗不再问宿主，级别由插件的级别表列出。
  取消按发起方限定：只有曲目所属平台（即发起下载的那个插件）能取消它的下载。
- `protocol`：新增动作 `download-options`、`download`、`download-cancel`；新增 `DesktopDownloadOption`、`DesktopDownloadItem` 两个线上结构；`TaskSongDownload = "song-download"`。
  - `download-options` 动作与 `DesktopDownloadOption` 已被 `.scratch/download-level-fallback/spec.md` 删除。
- `plugintest.Host` 补上三个方法，供各插件测试使用。

**与第一轮回答的两处偏差**（以本文件为准）：

- 契约里**没有** `Bitrate`：宿主只拿得到 `Stream{Type,Size,Level}`，拿不到真实码率；128k/320k 这类文案本来就属于平台知识，留在插件的标签里。
- `DownloadOptions` 不做请求-响应，而是"请求 + 状态回推 + token 关联"：SDK 的宿主服务是排队即发即忘的（`desktop/plugin_client.go` 的 `send`/`commands`），插件在 UI 线程上等不到同步返回值。

## 宿主侧实现

- `internal/app/download_options.go`：探测 + 5 分钟 memo + 原因文案；只探测一次即可得到格式与大小。原因只有两句，而且都不猜测用户的状态：平台把这首歌服务到更低级别 → 「当前只能下载更低的音质」；这首歌根本播不了（`Privilege.Pl == 0` 或平台报不可用）→ 「需要登录或开通会员」；其余用平台自己的文案，再否则「该曲目没有这个音质」。
  - 该文件已被 `.scratch/download-level-fallback/spec.md` 整体删除：「当前只能下载更低的音质」不再存在（服务得更低照样下载），`downloadStream`/`downloadRefusal` 挪进 `downloads.go`，只剩后两句原因，`downloadStream` 命中播放缓存按服务级别查询。
- `internal/player/download.go`：取流、进度、取消与 URL 续签都走 SDK 的 `network.MediaSource`（与播放同一套 401/403 续签）；写 `.part` → 校验完成 → 原子改名到最终路径（不像缓存那样受容量上限约束）。完成条件现在写死在 `stream.go:130`（`d.size <= 0 || d.n == d.size`，Size 不符时既不落盘也不结束），且它是播放与缓存共用的，所以下载**另起一条取流通道**，用 `Content-Length` 判完整性（存在则必须相等）、无 `Content-Length` 时以 EOF 为准，失败整段重下最多 3 次（`pluginapi.Retry` 的 `{0, 1s, 3s}`）。播放与缓存的既有代码路径一行未动。
- 下载目录的可写性：宿主在入队前检查（`MkdirAll` + 写一个临时文件），插件在弹窗里对 `DesktopSettings.DownloadDir` 做同样的检查以禁用「开始下载」——契约里没有"可写"标志，两边各自检查同一个路径。
- 文件名清洗/截断/重名、`.lrc` 合并、封面嗅探与落盘同在此包。
- `internal/app/downloads.go`：串行队列、批次、取消、toast、`Downloads`/任务状态填充。
- `internal/app/plugin_rpc.go`：三个新动作；`platformHost`（`desktop_services.go`/`platform_host.go`/`playback_services.go`）实现对应方法并把 `DownloadAvailable` 置真。
- `internal/app/settings.go`：`DownloadDir` 字段与默认值、持久化。

## 插件侧实现（qqmusic）

- 入口：`pages.go:2072 trackMenu` 与 `main_window.go:202 songItems` 加「下载」（`DownloadAvailable` 为真才出现；正在下载的那首显示「下载中…」并禁用）。
- 弹窗：`ui.DialogBase`，列出探测到的选项（不可用置灰 + 原因），显示目录并提供「更改…」（`mygo.Dialog.Open{Directory:true}` → 走偏好补丁）与「打开」（`mygo.Shell.OpenPath`）；「开始下载」调 `h.host.Download(tracks, level)`。
- 设置窗：`settings_view.go` 的 `desktopSettings` 加「下载」分组（目录行 + 任务行），沿用现有 `group`/`row` 帮助函数。
- 偏好：`preferences.go` 加 `DownloadLevel string`。

## 开发流程

1. SDK 先合：`feat/song-download` 分支，完成后打 tag `v0.2.0`。
2. 宿主与 qqmusic 同一分支名；开发期各自 `go.mod` 加 `replace github.com/waygu-music-plugin/sdk => ../sdk`，随宿主一起验证。
3. 三个仓库各建 worktree（各自根目录 `.worktree/feat-song-download`，`.gitignore` 加 `.worktree/`），改动互不干扰。
4. 全部通过后：去掉两处 `replace`，宿主与 qqmusic 升依赖到 v0.2.0，按 `RELEASING.md` 的顺序发 SDK → 插件 → 宿主。

## 验收

- 宿主单测：文件名清洗/截断、同名 ` (2)`、临时文件→原子改名、完成判定（`Content-Length` 不符、无 `Content-Length`、`Size==0`）、`.lrc` 合并、未宣告能力时插件不显示入口（老宿主路径）。
- qqmusic `go test ./...`；SDK `go test ./...`（含 `plugintest`）。
- `LIVE=1`：真下载一首标准音质的歌，断言文件头（ID3/fLaC）、大小与服务端声明一致、同名 `.lrc` 存在。
- 手工：macOS 全流程（右键 → 弹窗 → 下载 → 设置窗任务区 → 完成后打开文件）；Windows/Linux 只报未验证。
