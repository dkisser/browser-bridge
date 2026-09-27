import {
  type Denial,
  denialKey,
  humanDenialMessage,
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
      <div className={styles.cardTitle}>
        {denial.origin !== undefined
          ? `${denial.command} — ${denial.origin}`
          : denial.command}
      </div>
      <div className={styles.cardMsg}>{humanDenialMessage(denial)}</div>
      <div className={styles.cardActions}>
        {denialCardButtons(denial).map(([action, label]) => (
          <button
            key={action}
            type="button"
            className={buttonClass(label)}
            onClick={() => onAction(action)}
          >
            {label}
          </button>
        ))}
      </div>
    </article>
  );
}

// Action styling follows the design.md approval triad: Reject = red wash,
// Allow Once = frosted neutral, Always/Session = emerald fill.
function buttonClass(label: string): string {
  if (label === 'Deny') return styles.actionDanger;
  if (label === 'Always' || label === 'Session') return styles.actionApprove;
  return styles.actionNeutral;
}
