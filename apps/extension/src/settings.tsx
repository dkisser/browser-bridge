import { createRoot } from 'react-dom/client';
import { formatHardConfig } from './settings-state';
import './ui/tokens.css';
import './ui/globals.css';
import styles from './ui/SettingsApp.module.css';

function SettingsApp() {
  return (
    <main className={styles.main}>
      <h1 className={styles.title}>Hard configuration</h1>
      <p className={styles.subtitle}>
        These values are baked into the installed bridge. Edits happen through
        file changes and re-running the installer, not this page.
      </p>
      {/* biome-ignore lint/a11y/useAriaPropsSupportedByRole: preserves the original config-list landmark label */}
      <div className={styles.configList} aria-label="Hard configuration values">
        {formatHardConfig().map((entry) => (
          <div key={entry.label} className={styles.configRow}>
            <div className={styles.configLabel}>{entry.label}</div>
            <div className={styles.configValue}>{entry.value}</div>
          </div>
        ))}
      </div>
      <p className={styles.note}>
        The state bar of the side panel links back to this page, and policy
        edits (origins, blocklist, approvals, takeover) stay on the side panel
        itself.
      </p>
    </main>
  );
}

const container = document.getElementById('root');
if (!container) {
  throw new Error('Missing required element: root');
}

createRoot(container).render(<SettingsApp />);
