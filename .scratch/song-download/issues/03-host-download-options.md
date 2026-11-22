# 宿主：级别探测与可用性判定

Status: resolved
Repo: 本仓库
Blocked by: 02

## 目标

给定一首曲目，宿主探测各音质级别的真实可用性、格式与大小，供插件弹窗展示。结果按（平台, 曲目 ref）缓存 5 分钟。

## 改动

- 新建 `internal/app/download_options.go`：
  - `downloadOptions(ctx, tr *pluginapi.Track) ([]pluginapi.DownloadOption, error)`：对 `pluginapi.Levels` 逐个调 `a.pl.ResolveStream`（`internal/app/app_platform.go:77`），读回 `Stream.Level/Type/Size`。
  - `Stream.Level` 低于请求级别 → `Available:false`，原因「当前只能下载更低的音质」（平台为什么只给到更低，宿主看不出来：会员、地区、曲库都一样）；这首歌根本播不了（`Privilege.Pl == 0` 或平台报不可用）→ 「需要登录或开通会员」；其它错误 → 平台自己的文案，没有则「该曲目没有此音质」。
  - 去重：同 (Level, Format) 只留一项（QQ 上 `higher` 与 `exhigh` 都落到 320k）。
  - memo：`map[struct{platform pluginapi.ID; ref string}]optionsEntry{at time.Time, opts []pluginapi.DownloadOption}`，5 分钟过期，读写加锁；平台上切换/插件重载时清空。
  - 探测串行、可被 `ctx` 取消；单次请求沿用宿主既有的超时与代理设置。
- 结果只回填到状态推送（由 05 接上），这里只提供纯函数 + 缓存。

## 验收

- 单测（假 adapter）：请求 `exhigh` 得到 `standard` 时该项不可用；请求 `hires` 时清单里没有 `hires`；同 (Level, Format) 去重后长度正确。
- 单测：同一曲目第二次调用不触发新的 `ResolveStream`（用计数假 adapter）；5 分钟后再次调用会重新探测（注入时钟，不依赖 `time.Sleep`）。
- 单测：`ctx` 取消后返回已有的部分结果，且这份部分结果**不会被记住**（下一次调用重新探测）。
- 单测：平台给出可用级别但 `Stream.Size==0` 时该项仍为可用、`Format` 取自探测结果，`Size` 留 0（界面显示「大小未知」）。

## Comments

宿主提交 `6cad295`，审查修复见 `812945d`。

审查后的实际行为：一次探测覆盖 `pluginapi.Levels`，按平台**真正**服务的级别成项；服务级别低于请求 → 不可用并给原因（`Privilege` 表明这首根本播不了时是「需要登录或开通会员」，否则平台自己的文案，再否则「该曲目没有这个音质」）；`(Level, Format)` 去重；宿主无法命名的级别直接不列（否则用户选了它，`Download` 会被拒）。答案按 `(平台, ref)` 记 5 分钟，过期即从内存删掉；**被取消的探测不记账**，免得残缺清单被记住 5 分钟。

验收：`internal/app/download_options_test.go`、`download_rpc_test.go`（token 经宿主动作往返）通过。
