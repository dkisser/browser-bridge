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
