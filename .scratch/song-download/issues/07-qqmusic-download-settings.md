# qqmusic：设置窗「下载」分组

Status: resolved
Repo: `~/Source/qqmusic`（`dhb52/qqmusic`）
Blocked by: 05

## 目标

设置窗里能看下载目录、改目录、看进行中与最近的下载、取消与打开文件。

## 改动

- `settings_view.go` 的 `desktopSettings`：在「缓存」分组后加 `group("下载", ...)`，只在 `state.DownloadAvailable` 时渲染，沿用现有 `row` 帮助函数。
  - 「下载目录」行：描述为当前生效路径（`state.DownloadDir`）；控件为「更改…」（`mygo.Dialog.Open{Directory:true}` → 经 `ApplyDesktopPreferences` 只改 `DownloadDir`）、「打开」（`mygo.Shell.OpenPath`）、「恢复默认」（把偏好置空串）。
  - 「进行中」行：曲名 + `Done/Total` 百分比（`Total==0` 时显示已下载字节数）+「取消」（`h.host.CancelDownload(id)`）。
  - 「最近」行：最多 5 条，曲名 + 状态（成功/失败原因/已取消）+「打开文件」（`mygo.Shell.ShowItemInFolder(path)`），无记录时不渲染该行。
- `preferences.go`：`platformPreferences` 加 `DownloadLevel string`（`json:"downloadLevel,omitempty"`）。

## 验收

- `go test ./...` 通过。
- 手工：改目录后立即生效（下一次下载落在新目录）；「恢复默认」后路径回到系统下载目录。
- 手工：下载一首并取消另一首 → 任务区分别显示进度、取消、失败原因；完成后「打开文件」定位到文件。
- 手工：`DownloadAvailable=false`（旧宿主）时「下载」分组不出现，`platformPreferences` 的既有字段照常保存。

## Comments

qqmusic 提交 `8e55482`、`20bac07`。

设置组按 `DownloadAvailable` 门控，进行中的一条显示进度与「取消」（按 `item.ID`），最近五条可「打开文件」；「恢复默认」在偏好已为空时禁用。审查还指出 `mygo.Shell.ShowItemInFolder` 不返回错误，所以定位失败无法上报：现在先 `stat` 文件，找不到就 toast「找不到下载的文件」，与「打开目录」失败的处理一致；级别文案表只留一份，设置窗的「音质」行改用同一个 helper。
