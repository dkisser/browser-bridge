# TODO

原列表中的所有条目均已于 2026-09-15 修复（#1 snapshot tier 误导、#2 `wait_navigation` 竞态、#3 虚拟化列表的区分报错、#4 full filter 格式收紧），条目删除，历史见 git log。

## 安全跟踪项（2026-09-22 review 后续）

以下两项为已知边界，属于"非本地/多用户"分支（ADR-0006 显式推迟），在多用户部署之前必须补齐；当前版本仅支持单用户回环（见 README Security）：

1. **WebSocket server 无 userId 归属校验**（M1）：`apps/websocket/src/server/index.ts` 的 `case 'response'` 向所有 `cliConnections` 广播响应，`case 'command'` 仅按 `browserId` 路由，未校验发起连接是否拥有目标 browser。需要在 server 侧建立 userId → browser 归属映射并按归属分发。
2. **鉴权为占位实现**（M2）：`packages/shared/src/auth.ts` 的 `NoopAuthProvider` 恒放行（`valid: true, userId: 'local', permissions: ['*']`），仅依赖回环绑定。非本地部署需接入真实鉴权（如强制 `ApiKeyAuthProvider`）并绑定回环。

其他后续硬化项（review Low，非阻塞）：

3. 配对 token 生命周期：当前为无到期/吊销的长期 bearer token（存储于 extension `chrome.storage` 与本地 `config.json` hash）。评估增加到期时间、显式吊销（popup 内）与通道绑定。
4. 敏感字段识别仍是启发式：自定义控件（无 `type`/`autocomplete`/`name` 特征的密码框）可能漏检。执行点二次复核已覆盖 TOCTOU，但无法识别从未声明敏感属性的控件。
5. extension 策略门（command → gate → consume）的端到端编排测试：现有单测覆盖 shared policy 与 bridge-core 内部 dispatch，缺 chrome.\* 编排层的并发消费与 recheck 拒绝路径测试。

## 用户体验改进项

以下不属于 bug 或安全缺口，而是 install/update 流程里已知会反复打扰用户的副作用，方便后续决定是否优化：

6. **Install/upgrade 强制重置 pairing**：当前 `install/install.sh:268` 在解压新 runtime 后无条件执行 `rm -f "$BB_HOME/config.json" "$HOME/.browser-bridge/config.json"`，下一次 `bridge-core` 启动拿不到 `extensionTokenHash`，会重新生成 `browserId` 并清空 pairing hash，所以 `bridge service update` 或重跑 `install.sh` 之后用户必须重新 `bridge pair`。文件内注释（line 257–267）解释这是 ADR-0011（bridge-core 合并 ws-server + local-proxy）时为丢弃老 schema 里 `apiToken` / `serverUrl` 等已无意义的字段而做的 force-reinit，但实现是无条件 rm，并不区分 pre-merge 与同架构升级。`bridge service restart` / `down && up` / CLI 操作不会触发，只有完整 install/upgrade 触发。改进路径：方案 1（删 install.sh 那两行 `rm`，依赖 `apps/bridge-core/src/state.ts` 已经在 `loadConfig()` 里做的"destructure 只保留 `browserId` / `extensionTokenHash`"自动丢弃老字段）是改动最小、与现有测试兼容性最好的版本；方案 2（给 `config.json` 加 `schemaVersion`，只在版本不匹配时 re-init）更稳健但需要前后端协同改动。
