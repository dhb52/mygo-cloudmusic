# 下载音质：插件静态列出，宿主按平台服务的级别降级

延续 `.scratch/song-download`（其决策 4 与契约段的 `RequestDownloadOptions` 部分由本文件取代）。

改前：弹窗打开前，插件把批次里每一首逐个交给宿主探测（每首最多 5 次 `ResolveStream`，`internal/app/download_options.go`），再聚合成「N/M 首可用」；请求的级别平台给不到时，宿主直接判失败（`downloadLowerOnly`「当前只能下载更低的音质」）。
改后：弹窗立即用插件自己的级别表列出音质；下载时平台服务什么级别，就落盘什么级别。

## 决策

1. **音质清单是平台自己的知识，弹窗不再问宿主。** 级别与文案本来就在插件里（`downloadLevels`），宿主的 `DesktopSettings` 只宣告下载能力与目录。弹窗与批次长度无关，立即出现。
   - 放弃的（明确接受）：`Size`/`Format` 预览、「N/M 首可用」、弹窗里预先标出「已下载过了/正在下载」。这些改在下载时给出：宿主逐首跳过并 toast（`downloadSkipReason` 已有），管理器里显示每首真实的级别，格式从落盘文件的后缀看得见。
   - QQ 的可选清单是 **标准（128k）、极高（320k）、无损（FLAC）**。不列 Hi-Res：`~/Source/qqmusic/internal/client/playback.go:35` 的请求链只有 `M500`(128k)、`M800`(320k)、`F000`(flac)，`hires` 走的是 lossless 的链，列出来就是假选项（旧探测下它必然显示「当前只能下载更低的音质」，本来也不可选）。给 QQ 补真正的 Hi-Res 取流是另一件事，范围外。
   - `higher` 与 `exhigh` 都请求 `M800`，平台回来的服务级别名是 `exhigh`：弹窗只列一个 `higher`（标签同为「极高（320k）」，QQ 上两者本来也是一项），记录与任务里写实际服务的级别 `exhigh`。
2. **降级发生在宿主取流处，不在弹窗里。** 请求级别只用于发起请求；落盘文件名、任务项、下载记录的级别都用平台真正服务的那一个。失败只剩「一点都取不到」（未登录 / 无会员 / 平台报错），沿用 `downloadRefusal` 的文案，逐条任务失败。
3. **探测契约整体删除，不留空壳。** SDK 的下载契约从未发布过（`v0.1.3` 不含它，`v0.2.0` 尚未打标签），现在改不构成对外的破坏性变更，因此不保留"静态版的 `RequestDownloadOptions`"这种壳。
4. 请求级别仍必须是 `pluginapi.Levels` 中的一个（`startDownloads` 的既有校验不动）。

## 契约（SDK，仍按 v0.2.0 发布）

删除（`~/Source/sdk`）：

- `pluginapi/download.go`：`DownloadOption`。
- `pluginapi/preferences.go`：`DesktopSettings.DownloadOptions`、`DownloadOptionsToken`；`DesktopServices.RequestDownloadOptions`（连同那段"探测要网络、结果随快照推回"的注释）。
- `protocol/values.go`：`DesktopSettings.DownloadOptions`/`DownloadOptionsToken`、`DesktopDownloadOption`。
- `protocol/messages.go`：`HostRequestDownloadOptions`（`download-options`）。
- `bridge/wire.go`：两处 `DownloadOptions`/`DownloadOptionsToken` 映射；`bridge/wire_test.go` 里往返断言这两项的两段。
- `desktop/plugin_client.go`：`RequestDownloadOptions`；`plugintest/host.go`：同名空实现；`desktop/plugin_client_download_test.go` 里断言它发出 `download-options` 的用例。
- `README.md:76`：那段请求-响应说明，改成"级别由插件列出，宿主按平台实际服务的级别落盘"。

保留并改注释：`Download`、`CancelDownload`、`DownloadItem`（`Level` 注明是**平台实际服务**的级别，可能低于请求）、`DesktopSettings.DownloadAvailable`/`DownloadDir`、`pluginapi.Levels`（仍是插件列级别与宿主校验的同一份顺序表）。

## 宿主实现（本仓库）

- 删 `internal/app/download_options.go`：`downloadOptionsCache`、`downloadOptions`、`probeDownloadOptions`、`downloadStreamFor`、`downloadOptionList`、`downloadOption` 及其键值全部去掉；`downloadStream`、`downloadRefusal` 挪到 `downloads.go` 的 `runDownload` 旁边（只有它用），`downloadLowerOnly` 一并删掉。
- `downloadStream` 去掉级别比较：取到流即成功，`stream.Level` 为空时补成请求级别（`ResolveStream` 的既有约定），不再返回 `downloadLowerOnly`。
- `runDownload`：
  - 解析出流之后，用**服务级别**回填 `job.item.Level`（新增 `downloads.setLevel(job, level)`，与 `setProgress` 同型、在应用线程上写，避免 worker 直接改 `job.item` 的数据竞争）；这样排队/进行中的条目在管理器里显示的就是真实级别，`finish` 沿用 `job.item.Level` 不用改。
  - 缓存命中改用服务级别查询：`cachedDownload(a.pl.Cache, job.platform, &job.track, stream.Level, stream.Type)`（原来按请求级别查，请求无损、平台只给 320k 时会白白走网络）。
- `downloads` 结构去掉 `options`、`token`、`optionList` 与 `optionSnapshot`/`setOptions`；`newDownloads` 去掉 `options` 初始化。
- `startDownloads`：去掉逐首 `options.forgetKey(...)`（及其注释），`downloadSkipReason` 的逐首跳过不动。
- `finishDownloadAt`：去掉 `a.downloads.options.forgetKey(...)`。
- `app_platform.go` `deactivatePlatformView`：去掉 `a.downloads.options.forget()`。
- `desktop_services.go`：去掉 `state.DownloadOptions`/`DownloadOptionsToken` 的赋值（第 42 行）与 `platformHost.RequestDownloadOptions` 方法。
- `plugin_rpc.go`：去掉 `protocol.HostRequestDownloadOptions` 分支（常量已删）。
- `download_records.go` 的 `downloadLevelLabel` 不动：记录里的级别现在就是服务级别。
- 测试：删 `internal/app/download_options_test.go`，以及 `download_records_test.go` 里 `TestDownloadOptionsRemindOfWhatIsKeptOrComing`（那正是"弹窗预告已下载/正在下载"的用例，行为改到入队时逐首跳过，已由 `downloads_test.go` 的 `downloadSkipReason` 用例覆盖）。`downloads_test.go` 补三条行为测试（用既有 `downloadAdapter`/`streamAtLevels` 假平台）：平台只服务 `standard` 时请求 `lossless` 仍下载，落盘扩展名、条目与记录的级别都是 `standard`；播放缓存里有 `standard` 的副本时，请求 `lossless` 也直接复制（不再按请求级别查缓存）；平台什么都不服务时失败并给出 `downloadRefusal` 的文案。`download_rpc_test.go` 删 `download-options` 的动作用例，保留 `HostDownload` 的归属/参数校验用例。

## 插件实现（`~/Source/qqmusic`）

- `download.go`：删 `downloadChoice`（含 `available`/`detail`）、`downloadChoices`、`pendingDownload`、`askDownloadOptions`、`downloadAnswers`、`downloadDialogState.choices`、`downloadChoiceRow` 的禁用与原因分支、`beginDownload` 里的 `downloadSeq`/`下载中` 之外的 `downloadPending` 判断、`downloadDialog` 顶部的 `downloadAnswers(state)` 调用与「暂时获取不到下载音质」空态。
- 新增一行级别表驱动弹窗：`downloadLevels` 里给每项补 `dialog bool`（`standard`/`higher`/`lossless` 为真，`exhigh`/`hires` 为假），弹窗直接按它为行，标签就是 `downloadLevelLabel`（两行详情不必要：128k/320k/FLAC 都已在标签里）。
- `beginDownload`：过 `downloadMaxTracks` 校验后立即 `open`（目录检查照旧：`d.dirChecked=false` 时首帧再查），不再有「正在检查音质…」状态；`downloadMenuItem`、`pageDownloadEntry` 去掉 `h.downloadPending != nil` 的分支（「下载中…」保留）。
- `defaultDownloadLevel(preferred, quality)`：在弹窗的静态行里选——记住的 `DownloadLevel` 命中就用它，否则用平台播放音质设置（`exhigh` 按同一档映射到 `higher`，两者都是 320k），否则用最近的档位（`downloadLevelRank` 的现有算法，去掉 `available()` 判断）。
- `home.go`：删 `downloadPending` 字段与重置处（`h.download.open, h.downloadPending = false, nil` 改成只重置 `open`）。
- 测试 `download_test.go`：删探测相关用例（token 往返、`host.requested`、`downloadChoices` 聚合、默认级别按可用性挑）；补两条：弹窗打开时**没有**任何 `RequestDownloadOptions` 往返（整批交给 `Download`）；点击「开始下载」把整批与所选级别交给 `host.Download`（例如 `lossless`）。批次上限、目录不可写、单曲菜单「下载中…」的既有用例保留。

## 文档

- `docs/adr/0001-song-download-ownership.md` 第三条后果（"下载的可用级别由宿主探测平台音源得出，探测结果缓存 5 分钟"）与新行为冲突，就地改写成"级别由插件列出，宿主按平台实际服务的级别落盘，请求的级别只是请求"（决定本身不变：下载仍由宿主执行）。
- `.scratch/song-download/spec.md`：决策 4 与契约段的 `RequestDownloadOptions`/`DownloadOptions`/`DownloadOptionsToken` 一段加指向本文件的注记；宿主实现段里 `download_options.go` 的描述同步。
- `.scratch/download-manager/spec.md` 决策 3 里"弹窗里就地标出已下载过/正在下载（`Available:false`）"改成"由宿主在入队时逐首跳过并 toast，管理器显示真实级别"。
- `GLOSSARY.md`：不改。「音质级别（Level）」已经写明"平台可能只给出低于所选档次的音源"；本改动只是把它变成下载时的常规行为，没有新词。
- `CHANGELOG.md`：下载尚未发布过（0.2.37 条目里没有下载），随下载首发的那个版本一并写"下载按平台实际提供的最高音质落盘"。

## 开工顺序

1. 三个仓库各开分支 `feat/download-level-fallback`（需要并行改动时按 `AGENTS.md` 用 `.worktree/`）。
2. SDK 先改契约（删探测），`go test ./...`。
3. 宿主改实现与测试，`go test ./internal/...`；qqmusic 改弹窗与测试，`go test ./...`（两处 `replace ../sdk` 保持到发布）。
4. `.scratch/song-download/issues/08-release-and-live-verification.md` 的步骤不变（先 SDK `v0.2.0`，再去 `replace`、升依赖），只是 SDK 在打标签前先删掉探测契约；其第 3 步的 LIVE 断言补一条：请求 `lossless` 下载一首只服务 320k 的歌，落盘是 mp3 且记录级别是 `exhigh`。
5. macOS 手工全流程（右键单曲 + 歌单「下载全部」）：弹窗立即出现、无「正在检查音质…」、开始下载后管理器/设置窗显示真实级别；Windows/Linux 只报未验证。

## 验收

- `go test ./internal/...`（宿主）、`go test ./...`（SDK、qqmusic）全绿。
- `git grep -n "RequestDownloadOptions\|DownloadOptions\|DownloadOption"` 在三个仓库里只命中级别表/`pluginapi.Levels` 相关的正当用法，不再有探测残留。
- 手工：弹窗无网络等待；一批歌里既有只有 128k 的也有无损的，选无损后两首都下成、管理器里分别显示「标准」「无损」。
- `LIVE=1` 真下载（需要本机 QQ 登录态）留下一次降级落盘的输出记录。

## 影响与取舍（写给自己看）

- 弹窗不再预告"这首你已经有了"：一首已下载的歌点「下载」会走到弹窗，开始后在列表里显示为已跳过 + toast。要回到预告就得重新逐首问宿主，正是这次要删的东西。
- 一批歌可能落成混合音质（无损的歌无损、只有 128k 的歌 128k），文件名与记录里都能看出各自的级别；这是"自动降级"的本意。
- `hires` 在 QQ 上仍不可下载；`pluginapi.Levels` 里有它，宿主对任何请求都只做"平台服务什么就落什么"，因此列不列它由插件决定（当前不列）。

## Comments

三个仓库都在 `feat/download-level-fallback` 分支上落地，未发布：

- SDK：删 `DownloadOption`、`DesktopSettings.DownloadOptions`/`DownloadOptionsToken`、`DesktopServices.RequestDownloadOptions`、`protocol.DesktopDownloadOption`、`HostRequestDownloadOptions`，桥接映射、`plugintest.Host` 空实现与 `README.md` 的那段请求-响应说明一并改掉。
- 宿主：`download_options.go` 与其测试删除；`downloadStream`/`downloadRefusal` 挪进 `downloads.go`，取流不再比较级别；新增 `downloads.setLevel`（worker 解析出流后 post 到应用线程，任务项与记录写服务级别）；播放缓存按服务级别查询；`desktop_services.go`/`plugin_rpc.go`/`app_platform.go` 的探测接线全部撤掉。
- qqmusic：`downloadLevels` 加 `dialog` 位（`standard`/`higher`/`lossless`），弹窗立即打开、只列标签；`pendingDownload`/`downloadChoices`/token 往返、菜单与页面入口的「正在检查音质…」全部删除。

验收跑过：`go test ./...`（SDK）、`go test ./internal/...`（宿主）、`go test ./...`（qqmusic）全绿；三个仓库里 `DownloadOption`/`RequestDownloadOptions`/`downloadPending` 一类符号已无残留（只剩文档里的历史注记）；宿主新测试覆盖"请求无损、平台只给 standard 时按 mp3/standard 落盘并在条目与记录里写 standard""缓存按服务级别命中"「平台什么都不给时报 `downloadNotServed`、不可播报 `downloadNeedsMembership`」。

留给人的部分：`.scratch/song-download/issues/08-release-and-live-verification.md` 的步骤（先打 SDK `v0.2.0`，再去两处 `replace`、升依赖），以及 `LIVE=1` 的真下载降级验证（需要本机 QQ 登录态）；Windows/Linux 未验证。
