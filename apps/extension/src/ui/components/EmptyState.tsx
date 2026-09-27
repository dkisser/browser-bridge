import styles from './EmptyState.module.css';

// Subdued empty-list line, matching the old `.empty` rendering. No
// role= attribute — the line is static text, and role="status" would
// steal announcements from the panel's message line (which is the
// actual live region).
export function EmptyState({ text }: { text: string }) {
  return <div className={styles.empty}>{text}</div>;
}
