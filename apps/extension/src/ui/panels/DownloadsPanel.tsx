import type { PendingDownload } from '../../policy-state';
import { EmptyState } from '../components/EmptyState';
import { toErrorMessage } from '../format';
import { handleDownloadAction } from '../policy-actions';
import styles from './panels.module.css';

interface DownloadsPanelProps {
  pendingDownloads: PendingDownload[];
  onError: (message: string) => void;
}

export function DownloadsPanel({
  pendingDownloads,
  onError,
}: DownloadsPanelProps) {
  return (
    <div>
      {pendingDownloads.length === 0 ? (
        <EmptyState text="No paused downloads." />
      ) : (
        pendingDownloads.map((download) => (
          <article key={download.id} className={styles.card}>
            <div className={styles.cardTitle}>
              {download.filename || download.url}
            </div>
            <div className={styles.cardActions}>
              <button
                type="button"
                className={styles.actionNeutral}
                onClick={() =>
                  void handleDownloadAction('resume', download.id).catch(
                    (error: unknown) => onError(toErrorMessage(error)),
                  )
                }
              >
                Resume
              </button>
              <button
                type="button"
                className={styles.actionNeutral}
                onClick={() =>
                  void handleDownloadAction(
                    'cancel-download',
                    download.id,
                  ).catch((error: unknown) => onError(toErrorMessage(error)))
                }
              >
                Cancel
              </button>
            </div>
          </article>
        ))
      )}
    </div>
  );
}
