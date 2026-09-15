# TODO

原列表中的 #1（snapshot tier 误导）、#2（`wait_navigation` 竞态）、#3（虚拟化列表的区分报错）均已于 2026-09-15 修复，条目删除，历史见 git log。

## 1. full filter 的格式收紧（2026-09-15 评审确定为缓办项）

**背景**

interactive 成为 snapshot 默认（见 ADR-0002）后，full 模式变为显式 opt-in 的非热路径。实测 full 模式输出有四处膨胀：缩进开销（depth 10 的行光前导空格 20 字符）、`generic` 包装行、文本双写（带 name 的 link/button 下再挂一层相同的 text run）、img 的 src 长 URL（10 个头像图 ≈ 1K 字符）。这些在 interactive 模式下已天然消失，是否还值得收紧 full 模式，等 full 的真实使用数据再定。

**可选方向（按难度排序，均已勘明）**

- **img src**：`content.ts` 的 `collectAttrs` 删 img 分支（2 行）。风险极低：alt 保留在 name，只损失 full 模式看图片 URL 的能力。
- **文本双写**：walker 已有 `suppressTextRuns`（content.ts）只挡直接文本节点，`<span>` 包装就漏。修复 = walk 时向下传递最近命名祖先的 name，text run 是其子串则跳过（约 15 行）。风险低-中：子串匹配有边界，但操作面下被丢的内容本已在 name 里。
- **缩进**：renderer 改 `INDENT` 或 depth cap（2-5 行），但输出形状全变，`packages/shared/tests/snapshot.test.ts` 的 33 个断言大部分要跟着改——机械但繁琐。
- **generic 包装合并**：renderer 合并单子链 generic 且保留带 ref 的行（30-50 行 + 边界规则）。唯一算"有点大"的项。

**相关文件**：`packages/shared/src/snapshot.ts`、`apps/extension/src/content.ts`、`packages/shared/tests/snapshot.test.ts`
