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
import { toErrorMessage } from './format';
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
  // Tracks whether the default-view selection has already been applied,
  // either from a real state read or from the error fallback. Subsequent
  // state changes re-render panel contents but must not yank the user off
  // the tab they chose — only an error → recovery transition re-applies.
  const defaultViewApplied = useRef(false);
  // Disambiguates the two ways defaultViewApplied could have been set:
  // once via the error fallback, the flag must be cleared again when
  // state finally arrives so the default view can re-route from 'approvals'
  // to whatever selectDefaultView actually returns (e.g. 'downloads' when
  // pending downloads exist and no denials do).
  const defaultedViaError = useRef(false);

  useEffect(() => {
    if (state !== null) {
      // Apply (or re-apply, after recovery from an error fallback) the
      // default view. After a clean first paint this is a no-op, so
      // subsequent storage updates do not yank the user's tab choice.
      if (!defaultViewApplied.current || defaultedViaError.current) {
        setActiveTab(selectDefaultView(state));
        defaultViewApplied.current = true;
        defaultedViaError.current = false;
      }
      return;
    }
    if (policyError !== null) {
      // No state available, but the initial read failed — fall back to the
      // Approvals tab so the panel is never blank. Mark this as an error
      // fallback so the state-driven branch above can clear the flag once
      // storage eventually delivers real state.
      setActiveTab('approvals');
      defaultViewApplied.current = true;
      defaultedViaError.current = true;
    }
  }, [state, policyError]);

  useEffect(() => {
    void updateBadge().catch((error: unknown) =>
      setMessage(toErrorMessage(error)),
    );
  }, [setMessage]);

  // --- Takeover: optimistic toggle, reverted on storage write failure ---
  const [takeover, setTakeover] = useState(false);
  // Tracks the requested value of an in-flight optimistic write so the
  // state-sync effect below can tell our own write landing (state
  // matches pending) apart from an external write (state differs from
  // pending and from the last applied value). Cleared on write failure.
  const pendingTakeoverValue = useRef<boolean | null>(null);
  // Last takeover value the UI has been synced to from storage. Lets the
  // effect suppress redundant setTakeover when our own write lands and
  // still detect external writes that differ from both pending and the
  // last applied value.
  const lastAppliedTakeover = useRef<boolean>(false);
  useEffect(() => {
    if (state === null) return;
    if (state.takeover === pendingTakeoverValue.current) {
      // Our optimistic write just landed; clear the pending flag and fall
      // through so the lastApplied branch updates the bookkeeping.
      pendingTakeoverValue.current = null;
    }
    if (state.takeover !== lastAppliedTakeover.current) {
      setTakeover(state.takeover);
      lastAppliedTakeover.current = state.takeover;
    }
  }, [state]);

  const handleTakeoverChange = useCallback(
    (desired: boolean): void => {
      setTakeover(desired);
      pendingTakeoverValue.current = desired;
      void setPolicyState({ takeover: desired }).catch((error: unknown) => {
        // Storage write failed; revert the switch so the UI matches persisted
        // state and the user is not misled about whether takeover is active.
        if (pendingTakeoverValue.current === desired) {
          pendingTakeoverValue.current = null;
        }
        setTakeover(!desired);
        lastAppliedTakeover.current = !desired;
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
    (action: DenialAction, targetKey: string): void => {
      void runDenialAction(action, targetKey).catch((error: unknown) =>
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
