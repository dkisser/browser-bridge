import styles from './EmptyState.module.css';

// Subdued empty-list line, matching the old `.empty` rendering.
export function EmptyState({ text }: { text: string }) {
  return (
    <div className={styles.empty} role="status">
      {text}
    </div>
  );
}
