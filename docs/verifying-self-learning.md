# Verifying self-learning by hand

The fixture baseline (`bridge memory bench run`, ADR-0025) proves the mechanism
works and puts a number on it. It cannot prove three things, because its site is
synthetic and its agent is a script:

1. that a card actually reaches a real agent's context,
2. that a real page's accessibility tree produces a *useful* card,
3. that nothing from the page leaks to disk on a real DOM.

This is how to check those by hand, cheapest first. Every step states what passing
looks like, because a manual check without a pass criterion is a tour.

## Before you start

```bash
bridge service up
bridge browser:list          # a browser must be connected
bridge memory list           # note what is already known; it should be empty
```

`BB_HOME` defaults to `~/.browser-bridge`; everything below is under
`data/` in it.

## One thing that will waste your time

**`bridge navigate` does not show a card, and that is not a broken build.** The
card is injected by the MCP adapter only. The CLI is a separate process talking to
the control plane over the WebSocket protocol, and the note is not on that wire at
all — it is `ResponsePayload.SiteNote`, tagged `json:"-"` (ADR-0024). So looking for
a card in terminal output is looking in the wrong place; use one of the four
surfaces below.

| Surface | Proves | Does *not* prove |
|---|---|---|
| `memory show <host> --resolve` | which containers resolve, and to which refs, on the last page seen | that a card was handed over |
| `memory show <host> --raw` | which containers were learned, ranked, and how often | that a card was handed over |
| `memory show <host>` | the wording an agent is given | the site map (see below) |
| `card_shown` records in the trace | that a card reached an agent | that the agent used it |
| the tool result inside your MCP client | that it is in the model's context | — |

`memory show` without `--resolve` deliberately does **not** print the site map. A
map whose refs were resolved against a page that is no longer there is a list of
handles that addresses nothing, and the command has no page to resolve against.
`--resolve` is the way to see it anyway: it reads the last page digest out of
the trace, resolves against that, and says so in its header *and* in the map's
own section title (ADR-0026). The refs it prints are real, and they are not valid
for whatever is in your browser now.

## 1. Does it learn from a real page? (terminal only, ~5 minutes)

Pick a site you have not visited under this feature yet.

```bash
bridge --browser <id> tab:new https://example.org
bridge --browser <id> --tab <tab> snapshot
bridge --browser <id> --tab <tab> gettext "@e<some container>"

bridge memory learn        # run the learner now instead of waiting for idle
bridge memory show <host> --resolve
```

**Pass:** the `map` holds several entries and they are ordered by `value`
descending. The largest `value` is the container on that page most worth reading.
The page has no notion of this — a folder sidebar, a toolbar and the main list
all accept a read successfully, and only the content-bearing one is worth much.
`--resolve` adds the refs those predicates land on, and the header names the page
it resolved against.

**Fail, and what it means:**

| Symptom | Means |
|---|---|
| `map` is empty after several reads | No evidence yet. A card needs either a confirmed ref-addressed call or a failure (ADR-0018's cold-start rule). Read one or two more containers and run `learn` again. |
| a `visual target` entry is ranked first | Content sizing is polluted — a screenshot's payload was used as the ranking signal (ADR-0025). |
| an entry has a role but no name, e.g. a bare `button` | A predicate that cannot pin itself down; after a redesign it silently resolves to whatever comes first. |
| no card at all for a site you clearly read | The learner has not run. `bridge memory learn`, and check `memory history` for whether the host is being written. |

## 2. Does anything from the page reach the disk? (terminal only)

This is the check the fixture cannot do, and the one that matters most, because
ADR-0018's whole claim is that the record is structural.

Take a sentence that appears verbatim on the page and is unlike anything else:

```bash
SENTENCE='把页面上那句独一无二的话原样粘在这里'

# read something that contains it, so the trace has a reason to
bridge --browser <id> --tab <tab> gettext "@e<the container>"

# the store must contain none of it
grep -rF "$SENTENCE" ~/.browser-bridge/data/ \
  && echo "LEAKED — note which file matched" \
  || echo "OK: nothing on disk"
```

**Pass:** `OK`.

If it leaks, which file matched is the location of the bug:
`stream.jsonl` means the capture path wrote it, `cards/<host>.json` means the
learner copied it out of the trace. What should be there instead:

- a URL is scheme + host + path — no query, no fragment (ADR-0025)
- a selector is its *shape*: `[data-message-subject=…]`, never the literal
- the browser's error message is not stored at all; only the error code is
- `type`'s text appears as a length, never as text

## 3. Does a *wrong* card degrade safely? (terminal + MCP client)

A card can be wrong. The file is plain, diffable JSON and is meant to be editable
by hand (ADR-0021), so break it on purpose.

```bash
cp ~/.browser-bridge/data/cards/<host>.json /tmp/card.bak

python3 - <<'EOF'
import json, os
p = os.path.expanduser("~/.browser-bridge/data/cards/<host>.json")
c = json.load(open(p))
c["map"][0]["pred"]["name"] = "Nonexistent Container"
json.dump(c, open(p, "w"), indent=2)
EOF
```

Check the cheap half first, with no browser involved:

```bash
bridge memory show <host> --resolve
```

**Pass:** the count in the header drops by one, and the entry you broke is gone
from the map rather than rendered with a ref. This is the same resolution the
injection does, against the same stored digest — only the caller differs, which
is why the diagnostic is trustworthy enough to lean on here.

Then the half that needs a real agent. Visit the site again from your MCP client
and watch what the agent is given.

**Pass:** the broken entry *disappears* — its predicate no longer resolves — and
if most of the card is gone the agent is told the notes no longer match, rather
than being handed a confident ref to the wrong control. Then: **the agent must
still complete the task.** A stale card may cost calls; it may not cost
correctness. This is the single most important property of the feature, and it is
the one a redesign of a real site will eventually exercise whether or not you
arrange it.

**Fail if** the agent acts on a ref that does not exist, or follows a stale
procedure into a dead end without falling back to reading the page.

Restore with `cp /tmp/card.bak`.

## 4. Does it help a real agent? (needs an MCP client)

You cannot run this one by hand, and it is worth being clear about why: *you* are
the agent in the terminal, and you cannot un-see a card once you have read it. So
the comparison has to be between two runs an LLM does without being told which is
which.

1. On a cold site **X**, give your agent the task in your MCP client. Count the
   tool calls it makes.
2. On a different cold site **Y**, do the same. This is your second cold sample —
   one is anecdote.
3. Return to **X** and repeat the task. This run has a card.

**Pass:** the warm run costs fewer calls, and never more. Do **not** expect the
fixture's ~2 calls: that is a number for a synthetic page with four plausible
containers. What transfers is the direction.

Cross-check rather than trusting your own count:

```bash
python3 -c "
import json, os
p = os.path.expanduser('~/.browser-bridge/data/stream.jsonl')
n = e = 0
for line in open(p):
    r = json.loads(line)
    if r.get('kind') == 'card_shown':
        n += 1
print(f'cards handed over: {n}')
"
```

If the agent says it used a card and this says zero, one of them is wrong. The
trace is the one that is right: an agent that ignored a card will report that it
was never there.

**This is also how you fill in `bridge memory bench record`** for a live baseline:

```bash
bridge memory bench record --host <host> --task "<same name every time>" \
  --ok --card used --since 10m
```

Use the same `--task` string across runs or the trend cannot group them. The call
and failure counts come from the trace, not from you; pass `--calls` as well and
it is cross-checked, because a window covering the wrong stretch of session is the
most common way a hand-rolled baseline quietly becomes wrong.

## 5. The whole thing at once

```bash
bridge memory bench run                 # fixture: the number, and the redesign curve
bridge memory bench report --mode fixture
```

Deterministic, needs no browser and no network, and safe to run in CI. If a change
moves the numbers, that is the mechanism and not the weather.

## Reading the result

`bridge memory history <host>` is the audit surface: every automatic update, what
evidence caused it, and why. Read it after step 3 — a run that produced `stale`
revisions on a site you never changed means the staleness signal is
misfiring, and that is worth knowing before a real site changes underneath a card
you trusted.
