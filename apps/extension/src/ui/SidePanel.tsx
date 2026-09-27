import {
  type MouseEvent,
  useCallback,
  useEffect,
  useRef,
  useState,
} from 'react';
import { type PolicyState, setPolicyState, updateBadge } from '../policy-state';
import { type SidePanelTab, selectDefaultView } from '../side-panel-state';
import { SegmentedNav } from './components/SegmentedNav';
import { TakeoverHero } from './components/TakeoverHero';
import { useBridgeStatus } from './hooks/useBridgeStatus';
import { usePolicyState } from './hooks/usePolicyState';
import { ApprovalsPanel } from './panels/ApprovalsPanel';
import { BlocklistPanel } from './panels/BlocklistPanel';
import { DownloadsPanel } from './panels/DownloadsPanel';
import { OriginsPanel } from './panels/OriginsPanel';
import {
  type DenialAction,
  type OriginKind,
  removeOriginEntry,
  handleDenialAction as runDenialAction,
} from './policy-actions';
import styles from './SidePanel.module.css';

const TABS = [
  { id: 'approvals', label: 'Approvals' },
  { id: 'origins', label: 'Origins' },
  { id: 'blocklist', label: 'Blocklist' },
  { id: 'downloads', label: 'Downloads' },
] as const;

export function SidePanel() {
  // --- Message line (persistent flag semantics preserved) ---
  const [message, setMessageText] = useState('');
  const messagePersistent = useRef(false);
  const setMessage = useCallback((text: string, persistent = true) => {
    // Don't overwrite a persistent user-action error with a transient
    // poll-failure message; that pair of writes clears the persistent
    // message and leaves the user with no feedback on their last action.
    if (messagePersistent.current && !persistent) return;
    setMessageText(text);
    messagePersistent.current = persistent;
  }, []);
  const clearTransientMessage = useCallback(() => {
    if (messagePersistent.current) return;
    setMessageText('');
  }, []);

  // --- Data sources ---
  const { state, error: policyError } = usePolicyState(setMessage);
  const { browserConnected, browserId } = useBridgeStatus(
    setMessage,
    clearTransientMessage,
  );

  const [activeTab, setActiveTab] = useState<SidePanelTab>('approvals');
  const defaultViewApplied = useRef(false);

  // First paint only: activate the default view once policy state loads.
  // Subsequent storage changes re-render contents but must not yank the
  // user off the tab they chose.
  useEffect(() => {
    if (state !== null && !defaultViewApplied.current) {
      defaultViewApplied.current = true;
      setActiveTab(selectDefaultView(state));
    }
  }, [state]);

  // If the very first storage read fails, fall back to the Approvals tab so
  // the panel is never blank.
  useEffect(() => {
    if (policyError !== null) {
      defaultViewApplied.current = true;
      setActiveTab('approvals');
    }
  }, [policyError]);

  useEffect(() => {
    void updateBadge().catch((error: unknown) =>
      setMessage(toErrorMessage(error)),
    );
  }, [setMessage]);

  // --- Takeover: optimistic toggle, reverted on storage write failure ---
  const [takeover, setTakeover] = useState(false);
  useEffect(() => {
    if (state !== null) setTakeover(state.takeover);
  }, [state]);

  const handleTakeoverChange = useCallback(
    (desired: boolean): void => {
      setTakeover(desired);
      void setPolicyState({ takeover: desired }).catch((error: unknown) => {
        // Storage write failed; revert the switch so the UI matches persisted
        // state and the user is not misled about whether takeover is active.
        setTakeover(!desired);
        setMessage(toErrorMessage(error));
      });
    },
    [setMessage],
  );

  const handleOpenSettings = useCallback(
    (event: MouseEvent<HTMLButtonElement>): void => {
      event.preventDefault();
      void chrome.runtime
        .sendMessage({ type: 'open-options' })
        .then((response) => {
          const r = response as { status?: string; error?: string } | undefined;
          if (r?.status === 'error' && r.error !== undefined) {
            setMessage(r.error);
          }
        })
        .catch((error: unknown) => setMessage(toErrorMessage(error)));
    },
    [setMessage],
  );

  const handleDenialAction = useCallback(
    (action: DenialAction, index: number): void => {
      void runDenialAction(action, index).catch((error: unknown) =>
        setMessage(toErrorMessage(error)),
      );
    },
    [setMessage],
  );

  const handleRemoveOrigin = useCallback(
    (kind: OriginKind, origin: string): void => {
      void removeOriginEntry(kind, origin).catch((error: unknown) =>
        setMessage(toErrorMessage(error)),
      );
    },
    [setMessage],
  );

  const policy: PolicyState | null = state;

  return (
    <div className={styles.app}>
      <div className={styles.topCluster}>
        <TakeoverHero
          takeover={takeover}
          browserConnected={browserConnected}
          browserId={browserId}
          paired={policy !== null && policy.pairingToken !== null}
          onTakeoverChange={handleTakeoverChange}
          onOpenSettings={handleOpenSettings}
        />
        {message !== '' && (
          <div className={styles.message} role="status">
            {message}
          </div>
        )}
        <SegmentedNav
          tabs={TABS.map((tab) =>
            tab.id === 'approvals'
              ? { ...tab, badge: policy?.recentDenials.length ?? 0 }
              : tab,
          )}
          activeTab={activeTab}
          onActivate={setActiveTab}
        />
      </div>
      <div className={styles.panelHost}>
        <section
          id="panel-approvals"
          role="tabpanel"
          aria-labelledby="tab-approvals"
          className={panelClass(activeTab === 'approvals')}
        >
          <ApprovalsPanel
            denials={policy?.recentDenials ?? []}
            onDenialAction={handleDenialAction}
          />
        </section>
        <section
          id="panel-origins"
          role="tabpanel"
          aria-labelledby="tab-origins"
          className={panelClass(activeTab === 'origins')}
        >
          <OriginsPanel
            origins={policy?.origins ?? {}}
            deniedOrigins={policy?.deniedOrigins ?? {}}
            onRemove={handleRemoveOrigin}
          />
        </section>
        <section
          id="panel-blocklist"
          role="tabpanel"
          aria-labelledby="tab-blocklist"
          className={panelClass(activeTab === 'blocklist')}
        >
          <BlocklistPanel
            blockedOrigins={policy?.blockedOrigins ?? []}
            onError={setMessage}
          />
        </section>
        <section
          id="panel-downloads"
          role="tabpanel"
          aria-labelledby="tab-downloads"
          className={panelClass(activeTab === 'downloads')}
        >
          <DownloadsPanel
            pendingDownloads={policy?.pendingDownloads ?? []}
            onError={setMessage}
          />
        </section>
      </div>
    </div>
  );
}

function panelClass(active: boolean): string {
  return active ? styles.panelActive : styles.panel;
}

function toErrorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}
