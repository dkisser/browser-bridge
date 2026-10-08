# The CLI recalls by pull — a mandated skill step, not a tool

ADR-0019 rejected a dedicated `site_recall` tool as the *only* recall path, because it "depends on the agent remembering to ask". ADR-0024 then recorded that, as built, the in-band injection is MCP-only, and ADR-0026 labelled `bridge memory show` a human-only offline diagnostic — "nothing here is ever handed to an agent". Two facts have changed since:

1. **The CLI+skill path is the primary documented interface.** The README is protocol-first, the skill is installed by default (ADR-0015), and the maintainer's own agents drive the bridge through the skill's CLI path — no MCP client is configured anywhere in the maintainer's own setup. A recall mechanism that only fires inside the MCP adapter therefore does not reach the audience the feature is for.
2. **A second consumer appeared that push can never serve.** Writing a crystallized Routine (ADR-0028) needs the site structure *before* any `navigate` has run — at coding time there is no landing, so there is no injection point. There must be a way to fetch a card on demand.

**Decision:** the pull path is sanctioned for agents. The browser-bridge skill mandates `bridge memory show <host> --json` at two moments: right after landing on a host on the CLI path (where no injection happens), and before writing or rehearsing a Routine. Amends ADR-0019 and ADR-0026.

The ADR-0019 objection does not apply here. What was rejected was pull as the *sole* mechanism, left to the agent's free choice under pressure. This pull is (a) a complement to push where push exists, and (b) a checklist step inside the skill — the channel ADR-0003 already counts on for skill-loaded agents — not a spontaneous decision. And the retrieval worry that motivates push ("the agent might ask the wrong question") dissolves structurally: the key is the host, the lookup is one file read, and "no card for \<host\>" is a normal, cheap answer rather than a recall miss.

## Consequences

- No new tool: `TestToolsListMatchesTheContract` and the golden fixture stay untouched, same as ADR-0019 promised for push.
- The push path is unchanged — MCP-only, 400-token cap, once per landing.
- ADR-0026's offline labelling stays: when `--resolve` renders refs, they are labelled as resolved against a *recorded* page, and agents are held to that label just as humans are.
