# Shell 环境排查指南

Browser Bridge 的安装脚本要求 Bash ≥ 4。macOS 系统自带的 `/bin/bash` 长期停留在 3.2.x，因此需要先升级到新版 Bash。本指南只保留 Bash 升级步骤；安装完成后不再需要额外配置 PATH 来加载 `bun` 或源码目录。

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
