import { type KeyboardEvent, useState } from 'react';
import { EmptyState } from '../components/EmptyState';
import { addBlockEntry, removeBlockEntry } from '../policy-actions';
import styles from './panels.module.css';

interface BlocklistPanelProps {
  blockedOrigins: string[];
  onError: (message: string) => void;
}

export function BlocklistPanel({
  blockedOrigins,
  onError,
}: BlocklistPanelProps) {
  const [blockInput, setBlockInput] = useState('');

  const submitEntry = (): void => {
    const entry = blockInput.trim();
    if (entry === '') return;
    void addBlockEntry(entry)
      .then(() => setBlockInput(''))
      .catch((error: unknown) => onError(toErrorMessage(error)));
  };

  const handleKeyDown = (event: KeyboardEvent<HTMLInputElement>): void => {
    if (event.key === 'Enter') submitEntry();
  };

  return (
    <div>
      <h2 className={styles.panelHeading}>Blocklist</h2>
      <div className={styles.addRow}>
        <input
          type="text"
          value={blockInput}
          placeholder="example.com"
          onChange={(event) => setBlockInput(event.target.value)}
          onKeyDown={handleKeyDown}
        />
        <button
          type="button"
          className={styles.addButton}
          onClick={submitEntry}
        >
          Add
        </button>
      </div>
      <div className={styles.divider} />
      {blockedOrigins.length === 0 ? (
        <EmptyState text="No custom entries." />
      ) : (
        blockedOrigins.map((entry) => (
          <div key={entry} className={styles.row}>
            <div className={styles.rowLabel} title={entry}>
              {entry}
            </div>
            <button
              type="button"
              className={styles.rowButton}
              onClick={() =>
                void removeBlockEntry(entry).catch((error: unknown) =>
                  onError(toErrorMessage(error)),
                )
              }
            >
              Remove
            </button>
          </div>
        ))
      )}
    </div>
  );
}

function toErrorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}
