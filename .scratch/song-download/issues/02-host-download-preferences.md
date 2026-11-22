# 宿主：下载目录偏好与能力宣告

Status: resolved
Repo: 本仓库
Blocked by: 01

## 目标

宿主存下载目录偏好、在桌面设置里宣告下载能力，并把偏好补丁的新位接上。此时还没有下载行为，只有可配置项与宣告。

## 改动

- `internal/app/settings.go`：`settings` 加 `DownloadDir string`（`json:"downloadDir,omitempty"`）；`defaultSettings()` 留空（空 = 系统下载目录）。
- `internal/app/settings_platform.go`：`platformSettings` 与 `pluginapi.DesktopPreferences` 的互转补齐 `DownloadDir`。
- `internal/app/plugin_rpc.go:185-193`：新位由 SDK 的 `ApplyTo` 处理，确认走通；补上"未知位整包失败"的既有行为不变。
- `internal/app/desktop_services.go:10`：`DesktopSettings()` 填 `DownloadAvailable: true` 与解析后的 `DownloadDir`（空值时用 `mygo.App.Path(mygo.PathDownloads)`；`~/` 展开；求值失败时回落到空串）。
- `internal/app/local_plugin.go:23-31`：不动 `Needs`（下载不进握手协商，见 ADR-0001）。

## 验收

- 单测：`DownloadDir` 空值时 `DesktopSettings().DownloadDir` 是系统下载目录；显式路径原样返回；`~/x` 展开为 home 下的 x。
- 单测：老版本设置文件（无 `downloadDir`）加载后仍为空、`save()` 后写回不改变其它字段。
- 手工：`go tool mygo dev` 打开设置窗，外部插件（老版本 fnmusic）不受影响；偏好补丁仍能保存外观等既有字段。

## Comments

宿主提交 `6cad295`（目录字段、能力宣告、插件设置里读回目录）与 `812945d`（审查修复）。

审查发现并修掉的真 bug：`DesktopSettings().Preferences` 漏填 `DownloadDir`，而偏好补丁路径从这份快照出发、`applyDesktopPreferences` 又无条件回写，于是任何不含 `PrefDownloadDir` 的补丁（例如切换外观）都会把下载目录清空。现在有回归测试 `TestDownloadDirSurvivesAnUnrelatedPreference`。另修：`~/` 展开在 Windows 上不生效（按 `os.PathSeparator` 截前缀）。

验收：`TestDownloadDirFollowsTheSetting`（系统目录/显式路径/`~/x`）、`TestDownloadDirIsKeptInSettings`（旧设置文件 + 写回保真）、`TestDesktopSettingsAdvertiseDownloads` 通过。
