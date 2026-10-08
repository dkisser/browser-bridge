# The boundary ADR-0025 described, and the one that is implemented

**Amends** [ADR-0025](./0025-what-the-baseline-taught.md) (the boundary it
recorded) and [ADR-0018](./0018-structural-trace.md) (the boundary ADR-0025
moved). Neither decision changes. What changes is that ADR-0025's account of
where the line sits does not match the code, in three places, and in each of
them the wrong side of the line is where a page's words end up.

ADR-0025 is not wrong about the judgement — it is right that a selector looks
structural and is not, right that the browser's not-found message quotes the
selector back, and right that the code generalises where the message does not.
What it got wrong is the claim that the three reductions close the boundary.
They close the two channels that were looked at. A review of PR #34 found the
third, and re-reading the code against ADR-0025's own wording found two more.

## 1. The error message was stored, and ADR-0025 said it was not

ADR-0025: *"the browser's error message is not stored at all. Nothing ever
rendered that message; it was stored because it was available. What remains is
the error code, which is the part that generalises."*

`RecordResult` assigned the wire `error` field to `ErrCode`. That field carries
`err.message` — the extension sends the bare error message, and
`selectorNotFoundMessage` embeds the selector in it. `failureOf` then used that
sentence as the failure signature, which is what `FailureEntry.Signature` is, so
the prose went into `cards/<host>.json` and `failureLine` printed it back to the
agent. The comment above `failedCall` — *"deliberately carries no copy of the
browser's error message"* — stated the opposite of the code next to it, which is
the kind of thing that survives review only when nobody reads the message
builders.

`errCode` now splits the two kinds of value that arrive on that field. The
control plane's own errors are already codes (`browser_offline`,
`cannot_buffer`, `sw_timeout`), as are the ones the extension mints
(`forbidden_sender`, `bb_sensitive_field_at_execution`); those pass through
untouched, because flattening them would throw away the difference between "the
browser was offline" and "no such element". Everything else is prose, and prose
is classified on the fixed head of the template literal that produced it, so the
argument it embeds is never read. Unrecognised becomes `unknown`, which is a
real answer: an unrecognised failure is still a failure.

`p.Message` is dropped outright rather than reduced. It is router-generated
prose on every path that sets it, but `chrome.runtime.lastError` can carry the
tab's URL, and a URL in a card file is the channel `safeURL` exists to close.

## 2. A multi-word selector was stored, because the space was a structural marker

`safeSelector` classified a bare string as text when it contained none of
`.#[]():>+~*=, `. The space is in that set. Every sentence of prose contains a
space, so every multi-word phrase failed the test, fell through to the structural
path, and was copied byte for byte — into `FailureEntry.Sel` and into the stream's
`args.selector`. `safeSelector("her lawyer private note")` returned its input.

That is precisely the input ADR-0025 names: *"`get_text "some prose"` is a legal
call whose selector* is *the prose"*. The function's own doc comment had already
worked out what a bare-text selector should reduce to — *"A bare-text selector has
no shape, so it reduces to the fact that it was text"* — and the test never ran.

The space is now not a marker, which makes one real selector indistinguishable
from prose: a descendant combinator with nothing else in it (`div span`). It
reduces to `…`. That is the trade, resolved toward the side that cannot leak: the
card loses "a bare descendant selector does not resolve here" as a *specific*
lesson and keeps "a selector of this shape did not resolve here" as a general
one, where the alternative is writing a page's sentence into a file that outlives
the visit. ADR-0025's cost — *a card can no longer be matched against the exact
guess that failed* — is now slightly wider than it read.

## 3. A link's href was a third URL channel

`safeURL` was applied to the `navigate` argument and to the snapshot URL. The
third was missed: the extension's `collectAttrs` attaches a raw `href` to every
link, `parseSnapshotLine` kept it, and `PredicateFor` copied it into
`Predicate.AttrVal`, which serialises as `MapEntry.Pred.av`. A card could
therefore hold

```json
{"role": "link", "name": "Inbox", "av": "https://mail.example.com/u/0/?token=SECRET123"}
```

`Predicate.String()` rendered it to the agent, and with `BRIDGE_MEMORY_API_KEY`
set, `compress.go` posted the rendered card to the model endpoint. ADR-0025's
reasoning about query strings — *"a session token, a document id or a redirect
target"* — applies identically here; the value was simply never run through the
function written for it. `attrValue` sends `href` through `safeURL` and leaves
`name` alone: a form field's identifier is a name the extension chose to read,
not prose the page was displaying, and "this site calls the box `q`" is exactly
the lesson that saves a call.

## What is now enforced rather than asserted

Each of the three was a claim in a comment next to code that contradicted it. The
leak boundary is now checked against the bytes on disk: `TestCardFileNeverCarries
PageProse` drives the real manager through record → learn and greps both
`cards/<host>.json` and `stream.jsonl` for a sentence and a token, then asserts
the reduction left the lesson behind — the failure is still recorded, under
`no_element`, with the selector reduced to `…`. That is the manual check in
`docs/verifying-self-learning.md` layer 2, run on every build instead of on
request.

The generalisation is the one worth carrying: a privacy boundary enforced by a
function is only as good as the paths that reach it, and this one had three. The
three that were missed were all found by reading the *consumers* — who reads this
field, and where does it end up — rather than by reading the function. The card
is the thing that outlives the visit, so the question to ask of any new field is
"what is this in the card, five minutes and one upgrade from now".