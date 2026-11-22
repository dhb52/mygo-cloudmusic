# SDK：下载契约

Status: resolved
Repo: `waygu-music-plugin/sdk`

## 目标

按 `.scratch/song-download/spec.md` 的「契约」一节加下载面：类型、偏好位、桌面设置字段、三个即发即忘的宿主服务、协议动作与线上结构。协议版本仍为 v1，改动全部为新增。

## 改动

- `pluginapi/download.go`（新）：`DownloadOption`、`DownloadItem`。
- `pluginapi/preferences.go`：`DesktopPreferences` 加 `DownloadDir`；`DesktopSettings` 加 `DownloadAvailable`、`DownloadDir`、`Downloads []DownloadItem`、`DownloadOptions []DownloadOption`、`DownloadOptionsToken`；`DesktopServices` 加 `RequestDownloadOptions(track Track, token string)`、`Download(tracks []Track, level string)`、`CancelDownload(id uint64)`。
- `protocol/desktop_preferences.go`：`PrefDownloadDir = 1 << 12`、`allDesktopPreferences`、`Valid`、`Changes`、`ApplyTo`。
- `protocol/values.go`：`DesktopPreferences.DownloadDir`；`DesktopSettings` 五个新字段；`DesktopDownloadOption`、`DesktopDownloadItem`。
- `protocol/desktop_tasks.go`：`TaskSongDownload = "song-download"`。
- 动作常量（`protocol/messages.go` 或相邻文件）：`download-options`、`download`、`download-cancel`；`PluginHostCall` 承载曲目与级别（chip/字段随现有风格）。
- `bridge/wire.go`：`WireDesktopPreferences`/`LocalDesktopPreferences` 补 `DownloadDir`；`PluginSnapshot` 的桌面设置与两份 `DesktopSettings` 映射补齐。
- `desktop/plugin_client.go`：三个方法走 `send`（即发即忘，别用 `callHost` 阻塞）；`DesktopSettings()` 已直接返回快照，无需改。
- `plugintest/host.go`：补三个方法的空实现。

## 验收

- `go test ./...` 通过（含 `plugintest` 自测）。
- 单测：`DesktopPreferencePatch.Valid()` 拒绝 `PrefDownloadDir` 以外的未知位、接受 `PrefDownloadDir`；`Changes` 只在 `DownloadDir` 变化时置位；`ApplyTo` 回写正确。
- 单测：一个 nil 快照的插件客户端调三个方法不 panic，且命令进入队列（`commands` 通道）。
- `bridge` 往返：`WireDesktopPreferences` → `LocalDesktopPreferences` 保真；含下载项的 `DesktopSettings` 往返保真。

## Comments

已实现并合入三个仓库的集成分支 `feat/song-download`。SDK 提交 `ec7b9bf`（契约）、`377e5e3`（线上字段）、`ac25d23`（`DownloadItem.Phase` 用 `protocol.TaskPhase`，与 `DesktopTask.Phase` 一致）。

验收：`go test ./...` 全绿（含新增的 `protocol/desktop_preferences_test.go`、`bridge/wire_test.go`、`desktop/plugin_client_download_test.go`）。协议版本仍为 v1；`PrefDownloadDir` 是第 13 位，未知位仍被 `Valid` 拒绝。
