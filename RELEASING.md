# 发布 Wagyu Music

宿主在 [vicnoah/mygo-cloudmusic](https://github.com/vicnoah/mygo-cloudmusic) 发版。SDK、内置飞牛和外置插件在 [waygu-music-plugin](https://github.com/waygu-music-plugin) 分别发布。

## 插件

先推送 SDK 的版本标签，再在插件仓库更新 Go module 依赖。飞牛音乐与 QQ 音乐发布源码标签，由宿主引用（两者都内置进宿主）；宿主升级这两个插件必须重新发版。外置插件仓库的 Release workflow 在 Ubuntu 交叉构建 Windows、macOS 和 Linux 的 amd64、arm64 版本，使用仓库 secret `PLUGIN_SIGNING_KEY` 签名发行清单，随后发布正式版。四个发行均完成后，运行[官方目录](https://github.com/waygu-music-plugin/registry)的 Publish catalog workflow；QQ 音乐的外置发行仍要照发，旧版宿主只能从目录里装它。网易云的 sherpa-onnx 资源由其独立的 GitHub Actions 工作流维护。

开发期宿主 `go.mod` 用 `replace` 把 SDK 与 QQ 音乐指到仓内的 `./sdk`、`./plugins/qqmusic`，`plugins/qqmusic/go.mod` 再指到 `../../sdk`。这三条 `replace` 跟着仓库走，打标签前不需要删；`require` 仍写最后一次发布的标签版本，跟着升级。

### SDK 与 QQ 音乐

这两个子目录在宿主仓库里开发，各自仍是独立 module，发布仍由它们在 [waygu-music-plugin](https://github.com/waygu-music-plugin) 的仓库完成，靠标签触发：

- 宿主 `v*` 标签推 [vicnoah/mygo-cloudmusic](https://github.com/vicnoah/mygo-cloudmusic)，走本仓库的 Release workflow。
- `sdk/v*` 与 `qqmusic/v*` 标签推 `dhb52/mygo-cloudmusic`。**Mirror** workflow（只在这个仓库执行）把 `sdk/` 或 `plugins/qqmusic/` 用 `git subtree split` 拆出来，校验拆分结果与子目录逐字节一致，去掉指向仓内目录的 `replace`（镜像仓库没有那些兄弟目录），编译一次，再用 secret `MIRROR_TOKEN` 把结果 force push 到 `waygu-music-plugin/sdk` 或 `waygu-music-plugin/qqmusic` 的 `main`，并推送版本标签（`sdk/v0.2.0` → `v0.2.0`）。标签触发那两个仓库自己的 Release workflow：SDK 发源码 release，QQ 音乐发六个目标的 ZIP 与签名清单。`MIRROR_TOKEN` 是 fine-grained PAT，对两个镜像仓库各有 `contents: write`，用 `gh secret set MIRROR_TOKEN` 在 `dhb52/mygo-cloudmusic` 里配置。
- 镜像按 `require` 写明的版本构建，所以发 `qqmusic/v*` 之前要先把 SDK 发出去，并把 `plugins/qqmusic/go.mod` 里的 SDK `require` 换成那个版本。宿主的 `require` 不受影响，`replace` 会盖掉它。
- 也可以用 **Mirror** 的 workflow_dispatch 手动跑：只给 `component` 就只做拆分与校验、不推送；再给 `tag`（必须指向当前检出的提交）才推送。

镜像仓库的 `main` 是宿主提交的子树拆分结果（同一份文件、同一条提交信息、重写过的提交 SHA），此后只在宿主仓库改这两个子目录，不要再 `git subtree pull`。

## Alpha

更新 `mygo.json` 和 `CHANGELOG.md` 中的版本，然后推送匹配的 `v*-alpha.*` 标签。Release workflow 在 Ubuntu 构建全部六个目标、上传校验和，并发布为预发行。也可以在 Linux 先验证本地构建，需要 Go、jq、zip、NSIS、hfsprogs 和 7zip。先运行 `bash scripts/build-dmg-tools.sh build/dmg-tools`，并把生成的工具目录加入 PATH。

```bash
go test ./internal/...
bash scripts/build-alpha.sh
```

macOS 提供 DMG 和含 `.app` 的 ZIP，Linux 提供 DEB 和 tar.gz。两个架构的 macOS 包均由 Ubuntu 构建，未做 Apple 公证。Windows 安装程序未签名。Alpha 通过 GitHub Release 手动下载。

## 稳定版

提交源码及版本，推送稳定版的 `v*` 标签。GitHub Actions 在 Ubuntu 上构建全部六个目标，使用 `MYGO_UPDATER_PRIVATE_KEY` 签名更新清单，上传完成后发布为最新稳定版。macOS 提供 DMG 和包含 `.app` 的更新归档。Linux 提供 DEB、RPM 和更新归档。

## 为已发布版本补安装包

运行 **Supplement release installers** workflow，输入已发布标签。它使用发布时的 macOS 应用归档制作 DMG，并从 Alpha 标签的源码构建 DEB。新增资产会纳入该版本的校验和。

## MPV 解码组件

运行 **MPV components** workflow，输入组件版本。Ubuntu runner 生成六个独立 ZIP，使用 `MPV_RELEASE_SIGNING_KEY` 签名索引，然后更新 `mpv-components-v1`。宿主固定信任该签名密钥对应的公钥。发布 Alpha 前，可运行 `LIVE_MPV=1 go test ./internal/player/mpv -run TestLiveMPVComponent -v` 来验证当前官方包的下载、解码播放、停用和重启清理。
