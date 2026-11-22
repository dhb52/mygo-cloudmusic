# qqmusic：设置窗里的「下载管理器…」入口

Status: resolved
Repo: `/Users/barry/Source/qqmusic/.worktree/feat-song-download`
Blocked by: 02

## 目标

设置窗「下载」组里能直接打开宿主的下载管理器（看不到已下载/排队的地方，用户不会知道那群东西在宿主的窗口里）。

## 改动

- `settings_view.go` 的 `downloadSettings`：「下载目录」行之后加一行「下载管理器」，控件为按钮「打开」，调 `h.host.OpenDownloads()`；沿用现有 `row`/`actionPill` 风格。
- `download.go`：若已有 `openDownloadDir` 之类的帮助函数，同样处理错误与提示。

## 验收

- `go test ./...` 与 `go vet ./...` 通过。
- 单测：`DownloadAvailable` 为假时该行不出现；为真时按钮把 `OpenDownloads` 调出去（沿用记录式 Host 的既有写法）。
- 手工：点「打开」后宿主的下载管理器窗口出现在前台。

## Comments

qqmusic 提交 `36d3ab2`：设置窗「下载」组里的「打开下载管理器」按钮（按钮文案加长以免与下载目录那行的「打开」撞车——测试里也因此精确点击）。`TestDownloadSettingsFollowTheHost` 断言它只开一次；`go test ./...`、`go vet ./...` 通过。
