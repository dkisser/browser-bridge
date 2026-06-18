# Shell 环境排查指南

Browser Bridge 的安装脚本需要 Bash ≥ 4，而项目日常开发通常使用 zsh。本指南整理 macOS 上常见的 shell 相关问题：Bash 升级、PATH 顺序、默认 shell 切换等。

---

## macOS 升级 Bash 指南

`install.sh` 要求 Bash 版本不低于 4.0。macOS 系统自带的 `/bin/bash` 长期停留在 3.2.x，因此需要先升级到新版 Bash。

### 为什么不能直接替换 `/bin/bash`

`/bin/bash` 受 **SIP（System Integrity Protection）** 保护，无法直接修改或替换。即使关闭 SIP 强行替换，macOS 系统更新也会将其覆盖，且可能破坏系统脚本依赖。因此正确做法是保留系统 Bash 不动，通过 Homebrew 安装新版并切换默认 shell。

### 升级步骤

#### 1. 安装新版 Bash

```bash
brew install bash
```

安装完成后，新版 Bash 位于：

- Apple Silicon（M 系列）：`/opt/homebrew/bin/bash`
- Intel Mac：`/usr/local/bin/bash`

#### 2. 查看已安装的 Bash

```bash
which -a bash
brew --prefix bash
"$(brew --prefix bash)/bin/bash" --version
```

#### 3. 将新版 Bash 加入 `/etc/shells`

macOS 只允许 `/etc/shells` 中列出的 shell 作为默认登录 shell。

Apple Silicon：

```bash
sudo sh -c 'echo /opt/homebrew/bin/bash >> /etc/shells'
```

Intel：

```bash
sudo sh -c 'echo /usr/local/bin/bash >> /etc/shells'
```

#### 4. 切换默认 shell

Apple Silicon：

```bash
chsh -s /opt/homebrew/bin/bash
```

Intel：

```bash
chsh -s /usr/local/bin/bash
```

按提示输入密码。完成后**重新打开终端或重新登录**生效。

#### 5. 调整 PATH 顺序

确保 Homebrew 的 `bin` 目录排在系统路径之前，否则 `bash --version` 仍可能找到旧版 `/bin/bash`。

编辑 `~/.bash_profile`：

```bash
nano ~/.bash_profile
```

添加或调整：

```bash
export PATH="/opt/homebrew/bin:$PATH"
```

Intel 用户对应为 `/usr/local/bin`。

使配置生效：

```bash
source ~/.bash_profile
```

#### 6. 验证

```bash
echo "SHELL=$SHELL"
echo "BASH_VERSION=$BASH_VERSION"
which bash
bash --version
```

预期输出类似：

```text
SHELL=/opt/homebrew/bin/bash
BASH_VERSION=5.3.15(1)-release
/opt/homebrew/bin/bash
GNU bash, version 5.3.15(1)-release
```

### 排查：已经执行了 `chsh`，但 `bash --version` 仍显示 3.2.x

这是 PATH 顺序导致的典型现象。以下是一次真实排查过程的整理。

用户反馈默认 shell 已改，但 `bash --version` 仍显示旧版。执行排查命令后得到：

```bash
$ echo "SHELL=$SHELL"
SHELL=/opt/homebrew/bin/bash

$ echo "BASH_VERSION=$BASH_VERSION"
BASH_VERSION=5.3.15(1)-release

$ bash --version
GNU bash, version 3.2.57(1)-release (arm64-apple-darwin24)

$ which -a bash
/bin/bash
/opt/homebrew/bin/bash

$ echo "$PATH" | tr ':' '\n'
/usr/local/bin
/System/Cryptexes/App/usr/bin
/usr/bin
/bin
...
/opt/homebrew/bin
```

分析：

- `SHELL=/opt/homebrew/bin/bash` 和 `BASH_VERSION=5.3.15` 说明当前终端已经在运行新版 Bash，`chsh` 已生效。
- 但 `PATH` 中 `/usr/local/bin`、系统路径都在 `/opt/homebrew/bin` 之前，因此执行 `bash --version` 时，系统按 PATH 顺序先找到了 `/bin/bash`。

解决：将 Homebrew 路径放在 `PATH` 最前面：

```bash
export PATH="/opt/homebrew/bin:$PATH"
```

重新加载配置后：

```bash
$ which bash
/opt/homebrew/bin/bash

$ bash --version
GNU bash, version 5.3.15(1)-release
```

### 临时绕过方案

如果你暂时不想切换默认 shell，可以直接指定新版 Bash 运行安装脚本：

```bash
/opt/homebrew/bin/bash install/install.sh
```

### Bash 升级检查清单

- [ ] `brew install bash` 已完成
- [ ] `/etc/shells` 中包含新版 Bash 路径
- [ ] `chsh -s <brew-bash-path>` 已执行并重新登录
- [ ] `~/.bash_profile` 中 `PATH` 将 Homebrew `bin` 排在前面
- [ ] `bash --version` 显示 5.x

---

## 排查当前终端使用的 Shell

安装脚本本身依赖 Bash（≥ 4），但安装完成后启动项目时，你的终端可能是 **zsh** 或 **bash**。`.zshrc` 只对 zsh 生效，`.bashrc`/`.bash_profile` 只对 bash 生效。如果环境变量（例如 `bun` 的 PATH）看起来没有生效，首先要确认当前终端实际使用的是哪个 shell。

### 查看当前 shell

```bash
echo "默认登录 shell: $SHELL"
echo "当前 shell: $0"
```

也可以直接查看账户配置：

```bash
dscl . -read /Users/$USER UserShell
```

### 为什么终端默认是 bash

macOS 从 Catalina（10.15）开始，新建账户默认使用 zsh。但如果你是从更早的系统升级上来的，或者之前手动执行过 `chsh -s /bin/bash`，账户的默认 shell 可能仍然是 bash。

### 切换到 zsh

```bash
chsh -s /bin/zsh
```

按提示输入密码。之后**完全退出终端**（按 `Cmd + Q`，不能只关窗口）并重新打开。

验证：

```zsh
echo $SHELL
# 应该输出 /bin/zsh
```

### 终端应用可能强制指定 bash

即使账户默认 shell 已是 zsh，终端应用仍可能被配置为启动 bash：

- **Terminal.app**：`Settings → General → Shells open with`，选择 `Default login shell`。
- **iTerm2**：`Settings → Profiles → Default → General → Command`，选择 `Login shell`。
- **VS Code 集成终端**：检查 `settings.json` 中的 `terminal.integrated.defaultProfile.osx`，确保是 `zsh`，或删除该配置以跟随系统默认。

### 切换后让配置生效

如果 `.zshrc` 里已经配置了 bun 等环境变量，切换到 zsh 后手动 source 一次：

```zsh
source ~/.zshrc
```

下次打开新终端时，`.zshrc` 会自动加载。
