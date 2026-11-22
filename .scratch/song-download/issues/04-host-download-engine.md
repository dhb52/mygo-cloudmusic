# 宿主：下载引擎与落盘

Status: resolved
Repo: 本仓库
Blocked by: 02

## 目标

把"从平台取流→写盘"的通路开给下载：严格的完成判定、临时文件与原子改名、文件名与附带文件、重试与取消。播放与缓存的既有语义不变。

## 改动

- 新建 `internal/player/download.go`：
  - `DownloadFile(ctx, stream, dest, progress) (int64, error)`：用 SDK 的 `network.MediaSource` 取流（与播放共用 401/403 续签），在目标目录建 `*.part`；EOF 后：`Content-Length > 0 && n != Content-Length` → 删临时文件并返回 `ErrDownloadShort`（含期望与实际字节数）；无 `Content-Length` 时以 EOF 完成；`n == 0` 也报不完整；否则 `os.Rename` 到最终路径（同目录，原子）。
  - **不动 `internal/player/stream.go`**：那里 `d.size <= 0 || d.n == d.size` 的完成条件是播放与缓存共用的语义（宁可不成缓存，也不把截断文件当完整），下载另走一条通道，互不影响。
  - 重试：整段重下最多 3 次尝试（`pluginapi.Retry` 配 `{0, 1s, 3s}`，`context.Canceled` 立即结束）。
  - `CopyFile(src, dest)`：缓存里已有这首歌时复制，走同样的 `.part` + 改名。
  - `ReserveTarget(dir, track, format, size)`：命名、清洗、截断、重名 ` (2)`、同名同大小报 `Exists`。
  - `WriteLyrics` / `WriteCover`：同名 `.lrc`（译文合并为同时间戳两行）与同名封面（按字节嗅探 jpg/png，已存在则跳过）。
  - `SongName(track)`：导出的文件基名，宿主用它给下载项命名。
  - `Cache.CachedPath(owner, track, level)`：给下载查缓存（导出的小接口）。
- 进度：`Progress func(done, total int64)`，总量取 `Content-Length`，未知时为 0。

## 验收

- 单测（`httptest`）：正常响应按 `Content-Length` 落盘，最终文件存在、临时文件消失。
- 单测：`Content-Length` 声明 100 实际发 60 → 失败、目录里没有残留文件、错误文本含期望与实际字节数。
- 单测：无 `Content-Length`（chunked）且 `Stream.Size==0` → 以 EOF 完成并落盘。
- 单测：取消后没有残留 `.part`，错误为 `context.Canceled`。
- 单测：`sanitize` 覆盖 `/ \ : * ? " < > |`、控制字符、超长中英混合（截断不破坏 UTF-8）、首尾点与空格。
- 单测：同名同大小 → 「已存在」且不覆盖；同名不同大小 → ` (2)`；封面与歌词跟随同一后缀。
- 回归：现有 `internal/player` 测试（缓存、播放、MPV 路径）全部通过，缓存落盘行为与今天一致。

## Comments

宿主提交 `ef90a4d`、`891548a`（重试）、`812945d`（EOF 也按 `Content-Length` 判完整）。

实现与工单的一处偏差（已写进 spec）：**没有动 `internal/player/stream.go`**。那里的完成条件 `d.size <= 0 || d.n == d.size` 是播放与缓存共用的语义（宁可不缓存，也不把截断文件当完整），下载另起一条取流通道（`DownloadFile`，同样走 SDK 的 `network.MediaSource`，因此共用 401/403 续签）。`main..HEAD` 的 diff 对 `stream.go` 零改动，`cache.go` 只新增 `CachedPath`/`TrackRef`/`findAtLocked`。

验收：`internal/player/download_test.go` 覆盖整首落盘、`Content-Length` 不符（含重试次数）、空响应、无 `Content-Length`、取消无残留、命名清洗与截断、同名同大小跳过、` (2)`、歌词合并、封面嗅探、`CopyFile`、`CachedPath`（含"不拿更高音质冒充"）。
