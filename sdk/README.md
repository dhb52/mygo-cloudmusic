# Waygu Music 平台协议 SDK

`github.com/waygu-music-plugin/sdk/protocol` 定义宿主与外置平台共用的线上类型，仅依赖 Go 标准库。`sdk/pluginapi` 提供插件本地接口，`sdk/desktop` 运行插件的 MyGo 事件循环，`sdk/platformrpc` 提供双向进程传输。独立 SDK 可在本目录执行 `GOWORK=off go test ./...`。

## 接入桌面运行器

在插件入口的 `main` 中调用 `desktop.RunPlatformPlugin(registration)`。一个程序提供多个入口时，使用 `desktop.RunPlatformPackage(packageID, registrations)`，例如 Subsonic 的四个入口。每条 `pluginapi.Registration` 提供稳定入口 ID、名称和 `Start`。`Start` 用 `Startup` 注册主窗、可选设置和桌面歌词，以及生命周期回调，返回提供媒体接口的适配器。

插件从独立 Go module 引入本 SDK。宿主和各平台插件通过 Go module 依赖 SDK 的发行版本。飞牛模块提供 `Registration()`，由宿主在编译时装配。外置插件由宿主启动独立进程并接收作用域。插件在本地构造 MyGo 视图，通过 `Startup.Host` 调用宿主。插件视图测试可用 `sdk/plugintest` 的宿主替身。示例入口见 [飞牛](https://github.com/waygu-music-plugin/fnmusic)、[Songloft](https://github.com/waygu-music-plugin/songloft) 和 [Subsonic](https://github.com/waygu-music-plugin/subsonic)。

## 建立连接和激活

传输使用 go-plugin 的 `ProtocolNetRPC`、gob 和 AutoMTLS。宿主校验程序 SHA-256，按 `platform` 名称取得插件端点，然后发送 `Plugin.Hello`。请求中的 `Identity` 包含包 ID 和入口 ID。宿主和插件均声明 `Version: 1`，并满足彼此的 `Needs`。握手接受当前协议，已保存的平台数据继续按原路径读取。

| 能力 | 提供方 | 用途 |
| --- | --- | --- |
| `window` | 插件 | 准备和管理插件原生窗口 |
| `media` | 插件 | 音源、封面、歌词、起播回调和队列续页 |
| `playback` | 宿主 | 播控、队列、暂停会话、播放快照和独立 PCM |
| `settings` | 宿主 | 平台配置、共享桌面设置和管理器入口 |
| `media-streams` | 双方 | 请求通道，传送封面并向提供方传播取消 |
| `media-renewal` | 双方 | 可选 HTTP 鉴权音源续期 |
| `playback-events` | 宿主 | 事件通道，推送带序号的播放快照 |
| `desktop-lyrics` | 宿主 | 桌面歌词偏好和托盘转发 |
| `desktop-tasks` | 宿主 | 共享设置操作的任务 ID、状态和结果 |
| `desktop-preference-patch` | 宿主 | 字段级设置更新 |

插件拨入宿主预留的反向 RPC 通道，宿主拨入插件等待的独立租约通道。宿主分配后续应用通道 ID。租约关闭后，插件退出事件循环，宿主在宽限期后结束仍存活的进程。

宿主为每个候选分配 `Scope`，并以 `Plugin.Prepare` 发送快照、平台配置和初始外观。此时作用域只允许读取快照。准备成功后宿主提交候选，再发送 `Plugin.Window` 的 `activate`。宿主在共享状态的应用线程校验连接和作用域。作用域撤销后，待应用的写入会被拒绝。

| 方法 | 请求 / 响应 | 动作 |
| --- | --- | --- |
| `Plugin.Hello` | `HelloRequest` / `HelloReply` | 协商协议、检查身份并绑定通道 |
| `Plugin.Prepare` | `PluginPrepare` / `PrepareReply` | 创建插件窗口和注册生命周期 |
| `Plugin.Window` | `PluginAction` / `PluginActionReply` | `activate`、`show`、`hide`、`settings`、`status` |
| `Plugin.Media` | `PluginCall` / `PluginMedia` | `stream`、`cast`、`cover`、`artwork`、`lyrics`、`started`、`feed` |
| `Host.Use` | `PluginHostCall` / `PluginHostReply` | `snapshot`，播控、会话、队列、PCM、设置及其他桌面动作 |
| `Host.Watch` | `WatchRequest` / `WatchReply` | 为已激活作用域预留专用播放事件流 |

插件可以通过 `Host.Use(manage)` 打开宿主的插件管理器。用户在管理器中切换平台。

准备回复的 `WindowActions` 必须包含 `activate` 和 `show`。插件按需声明设置和桌面歌词窗口操作。

有桌面歌词窗口的插件可声明 `lyrics-toggle` 和 `lyrics-lock`。宿主向该入口的 `activate` 请求附上已保存的锁定、样式和位置，并把托盘动作转给插件。插件拥有窗体和点击穿透，使用 `Host.Use(desktop-lyrics-save)` 把变更写回原设置。窗口位置以桌面逻辑坐标表示，`PositionSet` 区分未保存和坐标为零。撤销作用域后，宿主拒绝迟到的偏好写入。

桌面运行器在每个入口的 UI 状态目录保存主窗及设置窗。首次启动读取原宿主窗口位置，优先使用该入口早期版本写入的记录。安装探针使用独立临时目录。

宿主启动外置入口时可提供原应用的缓存路径。插件可通过 `storage.HostCachePath` 查找由进程内版本写入的本地文件，继续将新数据保存到入口自己的缓存。未提供路径的旧宿主和安装探针仍使用入口自己的缓存。

宿主在启动时指定凭据服务。正式版沿用原安装版的系统凭据库命名空间，开发版使用原数据目录下的文件。各插件继续用自己的实例键读写凭据。启动环境只含服务名，凭据内容由插件直接向本地凭据库读取。

## 数据与取消

- `Scope.Connection` 来自握手，`Activation` 来自宿主。曲目用平台实例及原始引用区分。曲目与暂停会话的位置用毫秒，播控和歌词快照中的 `time.Duration` 用纳秒。非零 `DeadlineNano` 是 Unix 纳秒的绝对期限。
- 快照请求可带已应用的 `QueueRevision`。宿主返回 `UnchangedQueue` 时，客户端保留上次应用的完整队列。跳转和进度操作可分别带观察到的队列版本和播放尝试 ID，过期操作会被拒绝。
- `Plugin.Media(feed)` 使用 `Ref` 指向当前续队句柄。`FeedCursor` 包含从零开始的批次号及宿主最新的首尾曲目 ID。宿主接受批次后才推进页码，临时失败后会重用同一游标。列表成功返回空批次代表结束。替换队列、退出电台或撤销激活时，宿主撤回对应请求。媒体请求流会把取消传播到子进程。
- 插件调用 `Host.Watch`，然后拨入宿主返回的流 ID。每个激活最多有一个订阅。流中连续编码 `PlaybackEvent`，首条包含完整快照及起始序号。其余事件各递增一个序号，队列版本未变化时可省略队列。宿主对进度和频谱作定时采样，并在空闲时发送保活快照。
- 插件收到断号、未知队列的增量或读失败时，关闭旧流并重新取得完整快照。宿主在积压达到 16 条或撤销激活时关闭流。
- 宿主将预留的请求流 ID 填入 `MediaStream`。插件拨入后解析媒体，在控制回复中返回 `CoverSize`，按该长度在流上传原始字节。每进程最多八个媒体请求并发传送，封面上限为 16 MiB。宿主提前关闭请求流会取消插件的提供方上下文。
- 协商启用 `media-renewal` 时，插件可在 `PluginSource` 设置 `Renewable`。HTTP 401 或 403 后，宿主把被拒绝的地址填入 `PreviousURL`，在同一激活中以原曲目、音质和投播格式重新发起 `stream` 或 `cast`。响应须保留媒体类型、音质及已知的长度和内容校验值。宿主用新地址和鉴权头重试一次，保留原请求方法和字节范围。
- `audio-open` 返回专用 PCM 流 ID、采样率及声道数。请求由小端 `uint8` 操作和 `int64` 参数组成。操作 1 定位至帧号，操作 2 请求交错样本数，每次至多 32768 个并按声道对齐。回复由 `uint8` 状态、`uint32` 样本数和相应的 `float32` 样本组成。状态 0–3 依次为成功、数据结束、授权撤销、解码失败。关闭流释放读句柄。

## 共享设置任务

解码组件下载和删除、歌曲缓存清理、DLNA 扫描与输出选择都返回 `DesktopTask`。ID 在宿主进程内唯一。每种操作的当前状态随播放快照和事件传递。已激活插件也可用 `Host.Use(desktop-task)` 和 `TaskID` 查询各操作的最新状态或最近 32 条终态记录。

`running` 表示进入执行，终态为 `succeeded`、`failed`、`canceled` 或 `replaced`。输出选择成功表示宿主已接受并切换目标，解码和设备起播结果继续由播放状态报告。取消下载时带当前 `TaskID`，过期请求不能取消新下载。删除解码组件会替换尚在运行的下载。

插件可用 `Host.Use(preferences-patch)` 发送 `DesktopPatch`。字段掩码选择此次更新的偏好，选中字段的 `false`、零和空字符串均为显式值。宿主按其当前设置合并后保存，未选中的字段保持原值。

## 歌曲下载

宿主用 `DesktopSettings.DownloadAvailable` 宣告下载能力，未宣告的宿主上插件不显示下载入口、弹窗和设置行。`DesktopSettings.DownloadDir` 是宿主解析后的生效目录；用户改目录时，插件用 `DesktopPatch` 的 `PrefDownloadDir`（`1 << 12`）写回，空字符串表示系统下载目录。

音质清单不用请求-响应：级别由插件自己按 `pluginapi.Levels` 列出，宿主的 `DesktopSettings` 只宣告下载能力与目录。`Host.Download(tracks, level)` 提交一批曲目，宿主串行下载，每批一个 `song-download` 桌面任务；`level` 只是向平台请求的档次，平台服务什么级别就落盘什么级别，因此每首的 `Level` 是平台实际服务的级别，可能低于请求。每首的进度与结果在 `DesktopSettings.Downloads`（`running`、`succeeded`、`failed`、`canceled`），`Ref` 是曲目在平台上的引用（判断"这首在不在下载中"用它，`Name` 是宿主清洗过的展示名），`ID` 用于 `Host.CancelDownload(id)`。两个调用都即发即忘，插件在 UI 线程上不等待网络。

## 错误

`Failure.Code` 为空表示成功。调用方按代码分支，`Message` 用于诊断。传输故障由 RPC 返回。

| 代码 | 含义 |
| --- | --- |
| `incompatible`、`invalid-prepare`、`missing-window` | 握手或窗口准备被拒绝 |
| `stale-activation`、`not-active`、`host-closing` | 作用域不再有效、候选尚未提交或宿主退出 |
| `deadline-exceeded`、`canceled` | 请求过期或取消 |
| `stale-playback-state`、`invalid-queue`、`stale-feed`、`missing-feed-cursor` | 播放版本已更新、曲目归属无效，或续页句柄已失效或缺少游标 |
| `feed-unavailable` | 当前续页请求失败，可重试同一批次 |
| `events-unavailable`、`events-busy` | 当前作用域没有事件能力或已有订阅 |
| `unknown-task`、`stale-task`、`decoder-busy` | 任务不可查、取消目标已更新或解码组件正在删除 |
| `cache-unavailable`、`cast-unavailable` | 当前宿主无缓存或投播控制器 |
| `unknown-track`、`unknown-entry`、`unknown-action`、`unsupported-window-action` | 曲目、平台或动作不可用 |
| `media-stream`、`invalid-media-stream`、`missing-media-stream`、`cover-too-large` | 媒体流建立、协商或载荷失败 |
| `invalid-source-renewal`、`media-source-changed` | 音源续期未协商、动作不支持或返回了不同的媒体 |
| `too-many-audio-readers`、`invalid-settings` | PCM 并发上限或平台配置格式错误 |
| `host`、`media` | 宿主或平台处理失败，详情见消息 |
