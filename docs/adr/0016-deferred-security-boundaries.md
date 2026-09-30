# Deferred security boundaries (M1, M2) of the local single-user positioning

The product targets one human, one browser, one machine, all loopback. Two
security boundaries that an explicit multi-user / non-loopback deployment
would have to close are *accepted* under that positioning and must be
revisited before any such deployment. They were previously tracked as
TODO items #1 and #2; this ADR captures the rationale, the concrete code
locations, and the revisit criteria so the constraints do not get lost
when the checklist is deleted.

## Considered Options

- **Close the boundaries now** (option A — full multi-user / non-loopback
  readiness): build a `userId → browser` ownership table on the WS server
  and replace `NoopAuthProvider` with a real provider before any release.
  Rejected: the product's positioning is local single-user; the work is
  substantial (server-side routing refactor + an auth surface + an
  extension-side token binding), and shipping it speculatively enlarges
  the surface for a deployment shape nobody has asked for. The threat
  model only requires it once a non-loopback or multi-user exposure is on
  the table — at which point revisit this ADR rather than rebuild from a
  checklist.
- **Delete the tracking items silently** (option B — what almost happened):
  without this ADR the M1/M2 wording lived only in `TODO.md`, which is
  intentionally ephemeral; deleting it would have erased the location
  pointers, the revisit criteria, and the pointer back to ADR-0006 that
  the README's Security section relies on. Rejected.
- **Capture the boundaries here, keep the local positioning** (chosen):
  one ADR, one list of (file, gap, revisit criterion) tuples. README's
  Security section keeps the single sentence pointing here; TODO.md is
  free to stay empty.

## Concrete boundaries

1. **M1 — WS server has no `userId`-to-browser ownership check**
   (`apps/bridge-core/internal/ws/inbound.go`). The response path
   broadcasts to every connected CLI; the command path routes only on
   `browserId`. There is no server-side check that the originating CLI
   owns the target browser. Acceptable under "one human, one browser,
   one machine" because there is only one CLI and one browser. Revisit
   when: (a) a second browser profile or a second user account is added
   to the same server install, or (b) the WS server is exposed beyond
   `127.0.0.1`.

2. **M2 — `NoopAuthProvider` is a placeholder** (`packages/shared/src/auth.ts`).
   It returns `{ valid: true, userId: 'local', permissions: ['*'] }` for
   every request. The defense in depth today is solely the loopback bind
   on ports 3001 / 3002 / 3003. Acceptable under "loopback only" because
   nothing on the loopback interface is untrusted. Revisit when:
   `BRIDGE_API_KEYS` is required (i.e. non-loopback deployment), or any
   non-loopback bind is added.

   > **Amended**: the premise "nothing on the loopback interface is untrusted"
   > does not hold, and the decision below rests on the *threat model* rather
   > than on that premise. A browser is not on the loopback interface, and a
   > page it renders can reach `127.0.0.1` without a click, a local process, or
   > an extension. WebSocket upgrades are not subject to CORS or preflight, so
   > the browser applies no gate.
   >
   > Concretely, on 3001 the inbound upgrade sets `InsecureSkipVerify: true`
   > (`internal/ws/inbound.go`), which disables the library's own cross-origin
   > rejection, and the bearer it defers to is `NoopAuthorizer` — now
   > `internal/ws/auth.go`, not the TS provider named above, since the Go
   > control plane is what serves the port. A hostile page can enumerate
   > browsers, request `tab:list`, and read every open tab's URL and title,
   > then issue the commands classified `UNRESTRICTED_COMMANDS`. The exposure
   > is bounded: the extension's policy gate still runs, so Takeover refuses,
   > and a page cannot mint `sensitive-field` or `submit` grants.
   >
   > The sibling server on 3002 does not have this problem — it rejects web
   > origins outright (`internal/ws/browserserver.go`) and requires a bearer
   > token. The asymmetry is unintentional and is what an audit noticed.
   >
   > **Accepted** by maintainer decision for the local single-user threat model
   > this product targets. The revisit criteria above cannot fire on their own
   > terms: they presuppose a non-loopback deployment, and this is not one.
   > The trigger that would actually matter is a change in use rather than in
   > configuration — serving more than one person or machine from this
   > control plane, or exposing it beyond the host. The fix when that day
   > comes is small and already exists on 3002: reject web origins on the
   > upgrade.

## Consequences

- README's Security section keeps its single-sentence pointer to this
  ADR via the "deferred branch of ADR-0006" wording. The wording is
  honest about the local positioning and not a promise that the
  boundaries are closed.
- `TODO.md` no longer needs to track M1/M2; the file can stay minimal
  (or be deleted entirely).
- Any future PR that adds a non-loopback bind, a second browser profile,
  a cloud ws-server, or a multi-user story must (a) update this ADR to
  mark the relevant boundary as closing, and (b) add a CHANGELOG entry.
- ADR-0006 stays the umbrella for "security enforcement lives in the
  extension"; this ADR is its explicit deferral appendix.
