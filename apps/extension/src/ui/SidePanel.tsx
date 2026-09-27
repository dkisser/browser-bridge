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
  // True once the user has manually picked a tab via SegmentedNav. Once
  // set, the default-view effect (and its error-recovery branch) must
  // never overwrite the user's selection. The error fallback below can
  // still set activeTab while this is false (panel would otherwise be
  // blank) but never once the user has interacted.
  const userPickedTab = useRef(false);
  // Disambiguates whether the default view has been routed via the
  // error fallback, so the state-driven branch can re-apply selectDefaultView
  // when the bridge eventually delivers state. Cleared on every successful
  // state read.
  const defaultedViaError = useRef(false);

  // Wraps setActiveTab so any tab change driven by the user marks the
  // selection as theirs. The default-view effect below checks this flag
  // before overwriting.
  const onActivateTab = useCallback((tab: SidePanelTab): void => {
    userPickedTab.current = true;
    setActiveTab(tab);
  }, []);

  useEffect(() => {
    if (state !== null) {
      // State is finally available — apply the default view as long as
      // the user has not picked one themselves. Re-apply on recovery from
      // the error fallback (no denials → 'origins', paused downloads →
      // 'downloads', etc.) but stop after one clean first paint so
      // subsequent storage updates do not yank the user off their tab.
      if (!userPickedTab.current) {
        setActiveTab(selectDefaultView(state));
      }
      defaultedViaError.current = false;
      return;
    }
    if (policyError !== null) {
      // No state available, but the initial read failed — fall back to the
      // Approvals tab so the panel is never blank. The state-driven branch
      // above re-routes to selectDefaultView once storage recovers, as
      // long as the user has not picked a tab themselves in the meantime.
      setActiveTab('approvals');
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
  // True while at least one storage write is in flight. While true, the
  // state-sync effect below assumes any state arrival is one of our own
  // writes (or a write from another surface that our optimistic update
  // has effectively superseded) and does not touch local state. Without
  // this gate, out-of-order write landing — or a second tab's write
  // landing during our pending write — would clobber the user's most
  // recent click.
  const hasPendingTakeoverWrite = useRef(false);
  // Last takeover value the UI has been synced to from storage, used to
  // detect external writes once no write is in flight.
  const lastAppliedTakeover = useRef(false);
  // Generation counter — bumped on every click. Lets the write-failure
  // handler tell whether a later click has superseded the failing one,
  // so it does not surface a stale persistent error banner for an
  // action that is no longer the user's current intent.
  const writeGen = useRef(0);
  const lastWriteGen = useRef(0);

  useEffect(() => {
    if (state === null) return;
    if (hasPendingTakeoverWrite.current) return;
    if (state.takeover !== lastAppliedTakeover.current) {
      setTakeover(state.takeover);
      lastAppliedTakeover.current = state.takeover;
    }
  }, [state]);

  const handleTakeoverChange = useCallback(
    (desired: boolean): void => {
      const myGen = ++writeGen.current;
      lastWriteGen.current = myGen;
      setTakeover(desired);
      hasPendingTakeoverWrite.current = true;
      void setPolicyState({ takeover: desired })
        .then(() => {
          hasPendingTakeoverWrite.current = false;
        })
        .catch((error: unknown) => {
          hasPendingTakeoverWrite.current = false;
          if (lastWriteGen.current !== myGen) {
            // A newer click has already superseded this one. The error
            // refers to an action that no longer reflects the user's
            // current intent — do not revert or surface a banner.
            return;
          }
          // Storage write failed and is still the user's latest action —
          // revert the switch so the UI matches persisted state, and
          // show the failure.
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
          onActivate={onActivateTab}
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
