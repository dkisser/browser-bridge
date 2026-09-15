# Show HN 发布文案

**发布时间建议：** 2026-07-08（周二）美西时间早上 7–9 点，对应北京时间周二晚上 10–12 点。若选周四，则对应 7-10 晚上。

---

## 标题

```text
Show HN: Browser Bridge – One browser backend for every AI agent
```

## 正文

```text
Hi HN,

There are two common ways to give an AI agent a browser today:

1. CDP-based tools (Playwright, Puppeteer, browser-use) launch a fresh Chrome profile. It works, but you lose your login state and keep re-authenticating.
2. Desktop agents like Claude, Cursor, Kimi, and Codex ship a Chrome extension paired with their own app. They reuse your Chrome, but each one installs its own extension and demands its own companion app — extension bloat, extra downloads, and duplicated setup.

Browser Bridge separates the browser from the agent. It's one Chrome extension + a local WebSocket bridge that turns the Chrome you already use into a reusable backend. Your sessions, cookies, and tabs stay where they are; agents send commands over a common protocol.

Because the protocol is exposed as a simple CLI and an MCP server, you can also embed it in n8n, Dify, or your own scripts — not just LLM agents. No per-agent extension, no extra desktop app you didn't ask for.

Quick start:

    curl -fsSL https://github.com/dkisser/browser-bridge/releases/latest/download/install.sh | bash

Load `~/Browser-Bridge/extension/` as an unpacked extension, then:

    bridge browser:list
    bridge navigate https://github.com --browser <browser-id>

Repo: https://github.com/dkisser/browser-bridge

I'd love feedback on whether a shared browser backend fits your agent workflows — and what non-agent use cases you'd try first.
```

---

## 极简备选版

**标题：**

```text
Show HN: Browser Bridge – Stop installing a new extension for every AI agent
```

**正文：**

```text
Hi HN,

CDP tools lose login state because they spin up a fresh browser. Desktop agents reuse your Chrome, but each ships its own extension + app — extension bloat and lock-in.

Browser Bridge is one Chrome extension + one protocol. Any agent, script, n8n flow, or Dify workflow can control your logged-in Chrome via a simple CLI or MCP.

    curl -fsSL https://github.com/dkisser/browser-bridge/releases/latest/download/install.sh | bash

Repo: https://github.com/dkisser/browser-bridge

What non-agent use case would you try first?
```

---

## 发布小贴士

- HN 标题不要 emoji，正文保持技术向、少营销感。
- 发完后尽快在评论区补充一个具体使用场景（例如“我让 Claude 直接打开 GitHub Notifications 并总结 PR”），更容易带起讨论。
- 如果选美西早上 9 点，对应北京时间当天晚上 12 点，也可以设 23:55 的提醒。
