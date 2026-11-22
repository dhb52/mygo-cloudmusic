# 发布：去 replace、升依赖、真下载验证

Status: ready-for-human
Repo: 三个仓库
Blocked by: 06, 07

## 目标

把开发期的本地 `replace` 换成正式版本，并按 `RELEASING.md` 的顺序发布；真下载一次作为端到端证据。

## 步骤

1. SDK 打 tag `v0.2.0`（先 `go test ./...`）。
2. 宿主与 qqmusic 去掉 `replace github.com/waygu-music-plugin/sdk => ../sdk`，`go get github.com/waygu-music-plugin/sdk@v0.2.0`，各自 `go test ./...`。
3. `LIVE=1` 真下载：qqmusic 里加一个受 `LIVE` 门控的端到端测试——下载一首 `standard`，断言文件头是 `ID3` 或 `fLaC`、大小与服务端声明一致、同名 `.lrc` 存在、目录里没有 `.part` 残留。
4. macOS 手工全流程；Windows/Linux 只报未验证。
5. 插件发版（Release workflow 交叉构建六个目标 + 签名清单），再按官方目录的 Publish catalog workflow 更新目录，最后宿主发版。

## 验收

- 三个仓库的 CI 全绿；`LIVE=1` 下载在 macOS 跑通并留下输出记录。
- `git grep -n "replace github.com/waygu-music-plugin/sdk"` 在宿主与插件里都没有命中（含 go.mod 注释掉的写法）。
- CHANGELOG 与 `mygo.json` 版本号按发布流程更新，发行说明写清：下载由宿主执行、下载目录在设置里配置、老宿主上的行为（不显示下载入口）。

## Comments

代码都已完成（SDK `feat/song-download`、宿主 `feat/song-download`、qqmusic `feat/song-download`），这一步留下的都是必须由人做的部分：

1. **SDK 还没有 `v0.2.0` 标签**，所以宿主与 qqmusic 的开发分支里各留了一条指向本地 worktree 的 `replace`（绝对路径，其他检出与 CI 都会因此编译不了）。顺序必须是：先推 SDK 的 `v0.2.0` → 再在宿主与 qqmusic 去掉 `replace`、升 `require`、各自 `go test ./...`。
2. **`LIVE=1` 的真下载验证没有跑**：需要本机有可用的 QQ 音乐登录态（`LIVE` 门控的测试才会执行）。要补的断言：文件头是 `ID3` 或 `fLaC`、大小与服务端声明一致、同名 `.lrc` 存在、目录里没有 `.part` 残留。
3. **手工验收**：macOS 全流程（右键 → 弹窗 → 下载 → 设置窗任务区 → 完成后打开文件）；Windows/Linux 未验证（`~/` 展开按 `strings.TrimPrefix(dir, "~")` 处理，已修掉按 `os.PathSeparator` 截前缀的写法，但未在 Windows 上实测）。
4. 审查已覆盖 SDK 与宿主（发现并修掉 3 个真 bug：批量队列卡住、偏好补丁清空下载目录、缓存命中把更高音质按请求级别命名）与 qqmusic 分支。

