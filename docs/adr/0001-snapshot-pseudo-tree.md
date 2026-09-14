# Snapshot: hand-rolled DOM pseudo-tree as the page representation

Agents need to read pages cheaply enough to fit in ~1k tokens by default, while keeping every element they might act on addressable. We decided the extension produces a Snapshot — a DOM-walked pseudo-tree of roles, names, chosen attributes, and refs — instead of shipping raw `innerHTML` or using the browser's accessibility tree. Representation, retention policy, and budget are ours to control, not Chrome's.

## Considered Options

- **Raw HTML (`gethtml` status quo)**: verbatim, unbounded over a pipeline with no size governance; script/style noise burns context for reading tasks. Kept only as an opt-in escape hatch.
- **CDP accessibility tree (`chrome.debugger`)**: faithful roles/states, but links arrive without hrefs, `data-*`/`id` are absent (so found elements can't be re-addressed), and the `debugger` permission shows Chrome's "debugging this browser" warning bar to users.
- **Readability-style markdown extraction**: best reading experience, but interaction targets detach from the text and two-format generation adds complexity; rejected because reading and acting must stay in one representation.
- **Cleaned HTML (strip script/style)**: keeps full fidelity but tag verbosity still dominates the token budget at ~1k scale.

## Consequences

- Elements are stamped with `data-bb-ref` during the walk; all element-addressing commands resolve ref > CSS selector > text match, so snapshot output feeds directly into clicks/types.
- Under a tight `max_chars` budget, pruning is priority-ordered: body text is cut first, interactive elements and headings last, with truncation stats returned so the agent knows to narrow via `selector`.
- Visibility/role fidelity is bounded by our walker, not by the platform's accessibility tree; pages with misleading DOM must be handled by tuning the role vocabulary, not by "trusting the browser".
