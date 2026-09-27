import type { PolicyState } from '../../policy-state';
import { EmptyState } from '../components/EmptyState';
import type { OriginKind } from '../policy-actions';
import styles from './panels.module.css';

interface OriginsPanelProps {
  origins: PolicyState['origins'];
  deniedOrigins: PolicyState['deniedOrigins'];
  onRemove: (kind: OriginKind, origin: string) => void;
}

export function OriginsPanel({
  origins,
  deniedOrigins,
  onRemove,
}: OriginsPanelProps) {
  const entries = [
    ...Object.entries(origins).map(([origin, scope]) => ({
      origin,
      scope,
      kind: 'origins' as const,
    })),
    ...Object.entries(deniedOrigins).map(([origin, scope]) => ({
      origin,
      scope,
      kind: 'deniedOrigins' as const,
    })),
  ];

  return (
    <div>
      {entries.length === 0 ? (
        <EmptyState text="No origins approved or denied yet." />
      ) : (
        entries.map((entry) => (
          <div key={`${entry.kind}:${entry.origin}`} className={styles.row}>
            <div
              className={
                entry.kind === 'deniedOrigins'
                  ? styles.rowLabelDenied
                  : styles.rowLabel
              }
              title={`${entry.origin} (${entry.scope})`}
            >
              {entry.origin} ({entry.scope})
            </div>
            <button
              type="button"
              className={styles.rowButton}
              onClick={() => onRemove(entry.kind, entry.origin)}
            >
              Remove
            </button>
          </div>
        ))
      )}
    </div>
  );
}
