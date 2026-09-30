# The model call is optional, compresses only, and needs no SDK

A browser tool that holds an API key is a thing people will ask about, so the
answer needs to be written down. The call exists for exactly one purpose:
turning a site card's stored call sequence into a shorter rendering of itself.
It never decides what is remembered, never judges whether an observation was
good, and is never in the path of recall.

**Decision:** one non-streaming `POST {base}/chat/completions` speaking the
OpenAI wire format, written directly against `net/http` and `encoding/json` —
no SDK, no agent, no tool loop, no streaming. Roughly 80 lines, consistent with
ADR-0021's reason for refusing a database. Configured by
`BRIDGE_MEMORY_API_KEY` plus optional `BRIDGE_MEMORY_MODEL` and
`BRIDGE_MEMORY_BASE_URL`.

**The feature is complete without it.** The stored truth is the observed call
sequence; compression is a view over it. If the key is unset, the call times
out, or the endpoint returns anything unexpected, the learner stores the
uncompressed sequence and the next run tries again. Compatibility comes from
the *protocol*, not the library, so pointing `BRIDGE_MEMORY_BASE_URL` at
OpenAI, another OpenAI-compatible gateway, or a local server is the only
configuration change required.

## Considered Options

- **`github.com/openai/openai-go`**: rejected for ADR-0021's reason one level up
  — it exists to cover the full OpenAI API surface, which shows in its request
  style (optional fields are wrapped in a generic `param.Opt`) and in its
  dependency count. Streaming, pagination, tool calls, and retry handling are
  all unused by a single non-streaming completion.
- **A community OpenAI client**: lighter than the official SDK, still a
  dependency for one request shape we can write directly.
- **Making the model call required**: rejected outright — it would make an API
  key a prerequisite for a feature whose entire point is reducing wasted
  exploration, and it would put a network call on the critical path of a
  background job that is otherwise pure local file analysis.
- **Deferring the call entirely to a later milestone**: defensible, and the
  shadow-mode-first ordering in the plan means V1 can ship without it. Recorded
  here so the reason for having it at all is not lost.

## Consequences

- Because ADR-0018 stores no page text and no typed input, **the outbound
  payload is commands and outcomes only** — there is no sensitive content on
  the machine to leak to a third-party endpoint. This is a property of the
  trace shape, not of the call, and it is the reason the two decisions belong
  in the same conversation.
- The provider choice is one environment variable, and switching providers is
  not a code change. This is also why the key does not come from
  `config.yaml`: that file belongs to the agent's runtime, and the control
  plane reading another system's config is a coupling this repo has no reason
  to take on (ADR-0016's single-user local posture assumes no such sharing).
- Promoting this to a real SDK later — when streaming, tool calls, or multiple
  endpoints are actually needed — is a contained change confined to this one
  function, because the wire format is already OpenAI's. Storage and recall do
  not move.
