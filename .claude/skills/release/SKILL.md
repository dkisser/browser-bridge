---
name: release
description: |
  发布 browser-bridge 的新版本（如 "发布 0.2.0"、"release v0.2.0"）。更新版本号与 CHANGELOG、跑全部检查、构建并冒烟验证发布产物、提交后 push tag 到 origin，由 GitHub Actions 触发正式 Release 并上传资产。仅适用于 Browser-Bridge（browser-bridge）仓库，在仓库 git 根目录下执行。
---

# Browser Bridge 发布

将仓库发布为指定版本：本地完成版本与产物准备，push tag 后由 GitHub Actions 构建并发布 Release 资产。

**仓库根目录不要写死**：用 `git rev-parse --show-toplevel` 定位（本技能曾硬编码旧路径导致误导），所有命令都在该目录下执行。

**关键点**：把 `vX.Y.Z` tag 推到 origin 会自动触发 `.github/workflows/release-*.yml`：
- `release-binaries.yml`：macOS **arm64** 运行时 tarball + sha256（x64 runner 已被禁用，x86_64 资产需要本地构建（步骤 4）后手动上传（步骤 6））
- `release-extension.yml`：扩展 zip + sha256
- `release-installer.yml`：自包含 `install.sh`

工作流会先校验根 `package.json` 版本与 tag 一致（BB-E030）、CHANGELOG 有对应条目（BB-E031），不一致则 Release 失败——所以步骤 3 必须做对再推。

## 输入

- 目标版本号，如 `0.2.0` 或 `v0.2.0`。用户没给就问。
- 必须是合法 semver `X.Y.Z`，且大于根 `package.json` 里的当前版本；已存在的 tag 直接中止。

## 发布步骤

以下都在仓库根目录（`cd "$(git rev-parse --show-toplevel)"`）依次执行：

### 1. 前置检查

```bash
git status --short            # 工作区尽量干净；记下已有的未提交改动，发布提交里只包含版本相关文件
git tag -l "v${VER}"          # 已存在则中止
grep -n '^## \[Unreleased\]' CHANGELOG.md   # 不存在则说明没有可发布内容，与用户确认
```

### 2. 质量门（与 CI 一致，全过才继续）

```bash
bun install --frozen-lockfile
bun run check                 # lint (biome + gofmt + go vet + golangci-lint) + tsc + 全部测试 (bun + go test -race)
bun run build:extension
bun run build:binaries        # 顺便验证编译产物可用（Go 静态二进制，ADR-0012 后无动态导入依赖问题）
```

`bun run test:install`（BATS）可选；按 AGENTS.md，若连续失败/挂起两次，改用 bash 直接验证，不要再跑 BATS。

### 3. 更新版本与 CHANGELOG

- 只改根 `package.json` 的 `version` 字段。注意：`apps/*/package.json` 和 `apps/extension/manifest.json` 里的版本号是独立的，发布工作流不校验它们，**不要顺手"修正"**。二进制版本由 build-binaries.sh 构建时从根 package.json 经 `-ldflags "-X main.version=..."` 注入，没有第二处需要同步。
- 把 `CHANGELOG.md` 中 `## [Unreleased]` 改为 `## [X.Y.Z] - <当天日期>`（YYYY-MM-DD），并在其上方插入一个新的空 `## [Unreleased]` 小节。

### 4. 构建发布产物

```bash
VERSION="v${VER}" bash .github/scripts/build-tarball.sh        # → 仓库根目录 tarball + .sha256（当前架构）
VERSION="v${VER}" bash .github/scripts/build-extension-zip.sh  # → 仓库根目录 zip + .sha256
bash .github/scripts/build-installer.sh dist/install.sh        # → dist/install.sh（自包含安装器）
```

需要 x86_64 资产时再跑一次 `VERSION="v${VER}" bash .github/scripts/build-tarball.sh x86_64`。

冒烟验证编译出的二进制真的能跑（不要只看编译成功）。ADR-0013 后只有一个二进制 `dist/bridge`，控制平面是隐藏子命令 `bridge serve`（`--help` 不列出属预期）。冒烟一律用临时 BB_HOME + 测试端口 3311-3313，**绝不动本机生产实例的 3001-3003，也不要 `bridge service down`**：

```bash
./dist/bridge --version                 # 应显示 tag 版本（ldflags 注入，见步骤 3）
./dist/bridge --browser test tab:list   # 服务未起时应报错并以非零退出（提示先 bridge service up）
BB_HOME=/tmp/bb-smoke BRIDGE_WS_PORT=3311 BRIDGE_LOCAL_PORT=3312 BRIDGE_MCP_PORT=3313 ./dist/bridge serve &
sleep 2
lsof -nP -iTCP:3311 -sTCP:LISTEN   # 控制平面 WS
lsof -nP -iTCP:3312 -sTCP:LISTEN   # 扩展桥接
lsof -nP -iTCP:3313 -sTCP:LISTEN   # MCP
kill %1; rm -rf /tmp/bb-smoke
```

注意：ADR-0012 起 `bridge service` 子命令直接编进 Go 版 `bridge` 二进制（不再有安装期 shell 包装器），`./dist/bridge service status` 可直接冒烟（service 名仍叫 bridge-core）。`serve` 若 `kill %1` 没杀掉（后台 shell 的 pid 可能拿不到），用 `lsof -t -iTCP:3311,3312,3313 -sTCP:LISTEN` 找到 pid 再杀，避免残留占用测试端口。

### 5. 提交、打 tag 并 push（push 前必须经用户确认）

push 是公开且基本不可逆的动作——**先向用户确认版本号和将要 push 的内容，再执行**：

```bash
git add package.json CHANGELOG.md
git commit -m "chore: release v${VER}"
git tag -a "v${VER}" -m "v${VER}"
git push origin "$(git branch --show-current)"
git push origin "v${VER}"     # 触发 release-binaries / release-extension / release-installer 三个工作流
```

### 6. 监控工作流，写 Release Note

push 后 tag 触发的三个工作流会自动创建 GitHub Release 并上传资产，但 **Release Note 是自动生成的占位内容，需要补上**。步骤：

1. 等待工作流完成（GitHub 上 Release 已创建）：

   ```bash
   gh run list --workflow=release-binaries.yml --limit 3
   gh run watch <run-id> --exit-status        # 三个 workflow 都要绿
   ```

2. 从 CHANGELOG 提取该版本小节写成 Release Note（去掉小节标题行，正文到下一个 `## [` 为止）：

   ```bash
   awk -v ver="${VER}" '$0 ~ "^## \\[" ver "\\]" {f=1; next} /^## / {f=0} f' CHANGELOG.md > "/tmp/release-notes-v${VER}.md"
   gh release edit "v${VER}" --title "v${VER}" --notes-file "/tmp/release-notes-v${VER}.md"
   ```

   发布前读一遍生成的 note：措辞面向用户，去掉内部引用（如 TODO 编号、ADR 路径）。

3. 若本地构建了 x86_64 tarball（GitHub 只出 arm64），手动上传：

   ```bash
   gh release upload "v${VER}" "browser-bridge-macos-x64-v${VER}.tar.gz" "browser-bridge-macos-x64-v${VER}.tar.gz.sha256" --clobber
   ```

4. 验证并汇报：`gh release view "v${VER}"`——确认资产齐全（arm64 tarball+sha256、x64 tarball（如有）、extension zip+sha256、install.sh）、Release Note 已生效。汇报内容：版本号、commit/tag、Release 页面链接、各工作流结果、本地产物路径。

## 升级兼容约定（改 install.sh / update 流程前必读）

`bridge [service] update` 是**跨版本契约**：已安装的旧 bridge 更新时总是下载**最新 release 的 install.sh** 来升级自己。因此最新 install.sh 必须一直能服务旧客户端传来的输入，破坏它就等于切断所有旧安装的升级路径：

- 旧版 `cmd_update` 固定传 `BB_VERSION=latest`（默认目标），install.sh 的 `resolve_version` 必须把 `latest` 当"查 GitHub API 取最新 tag"处理，不能只收 semver——v0.2.1 修过这个（BB-E022: invalid version 'latest'），回归测试在 `install/tests/install.bats`（"resolve_version treats BB_VERSION=latest ..."）。给 resolve_version 加新校验时，记得放行 `latest`。
- LaunchAgent label（`com.browser-bridge.bridge`）与 pidfile 路径（`~/.browser-bridge/run/`）是旧安装也在用的持久状态，不能改路径/改 label。
- plist 的 `ProgramArguments` 从旧版（`bridge up`）变成了 supervisor（`bridge service up --foreground`）。launchd 里可能残留按旧命令加载的 job：`bridge service up` 不能看到 label loaded 就算"already running"（v0.2.1 已修），改这块逻辑时保持"验证 live supervisor pidfile → 否则 bootout 重 bootstrap"的行为。

## 常见错误

- **BB-E030/BB-E031**（推 tag 后 release 工作流失败）：根 `package.json` 版本与 tag 不一致、或 CHANGELOG 缺少对应版本条目。push 之前就必须对齐（步骤 3）。
- **用户反馈 `bridge update` / `bridge service update` 报 BB-E022: invalid version 'latest'**：其安装版本过旧（v0.2.0 及更早的 update 默认参数从没工作过）。修复已随 v0.2.1 的 install.sh 发布，用户重跑一次 `bridge update` 即可自愈；或让其用显式版本 `bridge update vX.Y.Z` 绕过。
- **Release 上没有 install.sh / 资产缺 sha256**：某个工作流失败，用 `gh run list` 找到失败 run 看日志；修复后重跑该 workflow（`gh run rerun <run-id>`），不要重复推同一个 tag。
- **编译出的二进制缺功能**（如 MCP 端口 3003 没起来）：ADR-0012 后二进制是纯 Go 静态编译，不存在 xsschema 动态导入问题；先确认 3001-3003 没被别的进程占用，再看 `~/.browser-bridge/logs/`。
