import {
  type Denial,
  denialKey,
  humanDenialMessage,
  isOriginRequestReason,
} from '@browser-bridge/shared';
import { EmptyState } from '../components/EmptyState';
import { type DenialAction, denialCardButtons } from '../policy-actions';
import styles from './panels.module.css';

interface ApprovalsPanelProps {
  denials: Denial[];
  onDenialAction: (action: DenialAction, targetKey: string) => void;
}

export function ApprovalsPanel({
  denials,
  onDenialAction,
}: ApprovalsPanelProps) {
  return (
    <div>
      {denials.length === 0 ? (
        <EmptyState text="Nothing waiting for approval." />
      ) : (
        denials.map((denial) => (
          <DenialCard
            // denialKey (reason|origin|command) is the same stable id used
            // by recordDenial to dedupe; it survives prepends so the existing
            // card instance is reused. The onClick handler passes the same
            // key so handleDenialAction resolves the target inside the
            // serialized update — a positional index would point at the
            // wrong denial if a new one was prepended between render and
            // click.
            key={denialKey(denial)}
            denial={denial}
            onAction={(action) => onDenialAction(action, denialKey(denial))}
          />
        ))
      )}
    </div>
  );
}

function DenialCard({
  denial,
  onAction,
}: {
  denial: Denial;
  onAction: (action: DenialAction) => void;
}) {
  return (
    <article className={styles.card}>
      <div className={styles.cardHeader}>
        <div className={styles.cardTitle}>
          {denial.origin !== undefined
            ? `${denial.command} — ${denial.origin}`
            : denial.command}
        </div>
        {closable(denial) && (
          // ADR-0040: closing an origin request records that the human saw it
          // and does not want to answer it — a stance the agent reads on its
          // next attempt. Offered only where there is no Dismiss button
          // already, and never styled like Deny: red means refusal here, and
          // this is not one.
          <button
            type="button"
            className={styles.cardDismiss}
            aria-label="Dismiss this request"
            title="Not answering this one for now"
            onClick={() => onAction('dismiss')}
          >
            ×
          </button>
        )}
      </div>
      <div className={styles.cardMsg}>{humanDenialMessage(denial)}</div>
      <div className={styles.cardActions}>
        {denialCardButtons(denial).map(([action, label]) => (
          <button
            key={action}
            type="button"
            className={buttonClass(action)}
            onClick={() => onAction(action)}
          >
            {label}
          </button>
        ))}
      </div>
    </article>
  );
}

// Which cards carry the close. Kept as its own predicate rather than folded
// into denialCardButtons because that function is the copy-frozen answer
// set, and this is a different thing: the button set says what a human can
// *decide*, this says what a human can *put aside*.
//
// The origin requirement is not decoration: applyDenialAction records no
// stance for a denial with no origin, and a denial loses its origin whenever
// the gate reaches it with none (a content command arriving without a tab id
// resolves to origin null). Offering the close there would promise a stance
// and then quietly record nothing — the rejected pure-UI outcome, wearing a
// better icon.
//
// origin_ignored is included, which it was not at first. The card the human
// closed comes back on the agent's retry, and with no close on it they cannot
// remove the card they just closed — the badge stays at one for as long as the
// agent keeps asking. Pressing the close is a no-op on the stance, which is
// already recorded and unchanged by withStance replacing an identical entry
// with itself; it takes the card away, which is what pressing it asked for.
function closable(denial: Denial): boolean {
  return isOriginRequestReason(denial.reason) && denial.origin !== undefined;
}

// Action styling follows the design.md approval triad: deny-origin =
// red wash, allow-once = frosted neutral, approve-session / approve-always
// = emerald fill. Match by action enum (not by visible label string) so
// copy edits to the button text do not silently break the styling.
function buttonClass(action: DenialAction): string {
  if (action === 'deny-origin') return styles.actionDanger;
  if (action === 'approve-session' || action === 'approve-always') {
    return styles.actionApprove;
  }
  return styles.actionNeutral;
}
