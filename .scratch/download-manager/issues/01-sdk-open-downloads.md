# SDK：打开下载管理器

Status: resolved
Repo: `/Users/barry/Source/sdk/.worktree/feat-song-download`
Blocked by: 无

## 目标

给插件一个打开宿主下载 manager 窗口的电话，与既有的 `OpenSettings()`/`Manage()` 同型：即发即忘，不需要回复。

## 改动

- `pluginapi/preferences.go`（或 `platform.go` 的 `Services`）：`Services` 加 `OpenDownloads()`。
- `protocol`：动作常量 `HostDownloadManager = "download-manager"`。
- `desktop/plugin_client.go`：`s.action(protocol.HostDownloadManager)`。
- `plugintest/host.go`：`OpenDownloads()` 空实现。

## 验收

- `go test ./...` 通过。
- 单测：`pluginClientServices.OpenDownloads()` 在 nil 快照/无窗口时也把命令排进队列（沿用既有的下载方法测试写法），不 panic。

## Comments

SDK 提交 `6bcd81b`：`Services.OpenDownloads()` + 动作 `download-manager` + `plugintest` 桩；测试覆盖「调用会排队」。
