# 宿主：下载记录、重复拦阻与管理器窗口

Status: resolved
Repo: `/Users/barry/Source/mygo-cloudmusic/.worktree/feat-song-download`
Blocked by: 01

## 目标

(a) 持久化「已下载」记录并在入队前拦掉重复下载，(b) 让进行中的列表包含排队曲目，(c) 提供一个真正的下载管理器窗口（进行中 / 已下载 / 失败与已取消），(d) 接上 `download-manager` 动作与托盘入口。

## 改动

- `internal/app/download_records.go`（新）：`downloads.json` 的加载/保存（原子写）、`lookup`、`remember`、`forget`、`list`、`verify`（`os.Stat` 校验文件还在，结果缓存在内存里，打开管理器或入队时刷新）。
- `internal/app/downloads.go`：
  - 入队前：同一 (平台, ref) 正在排队或进行中 → 跳过并写「正在下载」；记录在且文件还在 → 跳过并写「已下载：<级别>」。
  - 成功落盘后 `remember`；`ReserveTarget` 报 `Exists` 时也 `remember`（补上用户手动放好的文件）。
  - 发布列表改为「正在下载 + 排队」全部（排队项 `Phase` = `running`、`Total` 用探测到的大小、`Done` = 0）。
  - 新增：`cancelAllDownloads()`、`retryDownload(id)`（失败项重下）、`clearDownloadRecords()`、`redownloadDownload(platform, ref)`（清记录后重下）。
- `internal/app/download_options.go`：探测时对「正在下载 / 已下载」的曲目，把可用的级别一并标成不可用并给出同样原因，于是弹窗里就能看到提醒（批量聚合行少算这些曲目，逐条原因解释清楚）。
- `internal/app/download_manager.go`（新窗口，仿 `plugin_manager.go` 的结构与配色）：下载目录行（更改/打开）、正在下载（含排队；进度、取消、全部取消）、已下载（打开文件、打开目录、重新下载、从列表移除、清空列表）、失败与已取消最近 5 条（原因、重试）。
- `internal/app/tray.go`：托盘菜单加「下载…」，打开该窗口；`internal/app/plugin_rpc.go`：新动作 `protocol.HostDownloadManager`；`platformHost` 实现 `OpenDownloads()`。
- `internal/app/app.go`：加载记录（`loadDownloadRecords`），窗口字段与既有窗口一同管理。

## 验收

- 单测（`downloads_test.go` / 新 `download_records_test.go`）：
  - 记录持久化：成功下载后写盘、重载后仍在、文件被删后 `verify` 标为「文件已不在」。
  - 重复拦阻：同一首连续两次 `startDownloads` → 第二次整批跳过，条目消息为「正在下载」；下载完成后再下一次 → 「已下载过了」。
  - 探测提醒：已下载的曲目其选项全部不可用，原因为「已下载过了」；正在下载的同理。
  - 发布列表：队列里三首时 `DesktopSettings().Downloads` 有三条（含排队，`Total` 来自探测）。
  - 管理器动作：清空列表只清记录（文件仍在）、文件不在时「重新下载」真的再下一份、文件还在时「重新下载」报「已存在」且不写第二份、记录在清理后回来。
- 手工：托盘「下载…」打开窗口；下载两首（一首在排队时取消）、下载完成后在「已下载」里打开文件、清空列表后文件仍在。

## Comments

宿主提交 `a1b6430`：记录（`internal/app/download_records.go`，`downloads.json`，按需 `os.Stat` 校验）、去重拦阻与探测提醒（`downloadSkipReason` + 缓存按曲目失效）、发布排队项、管理器窗口（`internal/app/download_manager.go`）与托盘入口。

审查期的两处判断：原因文案不写级别（级别怎么叫是平台的事）；「重新下载」在文件还在时不会写第二份（报「已存在」并补回记录），文件不在时才重下——这都是"不要重复下载"的本意，已写进 spec。

验收：`internal/app/download_records_test.go`（记录持久化/文件消失、正在下载与已下载的拦阻、探测提醒、队列可见、重试/清空/重新下载/全部取消）与既有下载测试全绿；`go test -race ./internal/...` 全绿。
