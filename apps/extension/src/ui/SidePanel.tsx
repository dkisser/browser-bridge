import {
  type MouseEvent,
  useCallback,
  useEffect,
  useRef,
  useState,
} from 'react';
import { type PolicyState, updateBadge } from '../policy-state';
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
import { requestPolicyOp } from './policy-ops';
import styles from './SidePanel.module.css';
import {
  click,
  createTakeoverSync,
  observe,
  settle,
  type TakeoverSync,
} from './takeover-sync';

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
    // An explicit empty string is a clear and always wins, even against
    // a currently persistent message — the dismiss button on the message
    // line relies on this. Otherwise: don't overwrite a persistent
    // user-action error with a transient poll-failure message; that
    // pair of writes clears the persistent message and leaves the user
    // with no feedback on their last action.
    if (text === '') {
      setMessageText('');
      messagePersistent.current = false;
      return;
    }
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

  // Start with no active tab so the panel does not paint the wrong default
  // (Approvals + 'Nothing waiting for approval.') on every open while the
  // initial policy read is in flight. The default-view effect below
  // sets it once state arrives.
  const [activeTab, setActiveTab] = useState<SidePanelTab | null>(null);
  // True once the user has manually picked a tab via SegmentedNav. Once
  // set, the default-view effect (and its error-recovery branch) must
  // never overwrite the user's selection.
  const userPickedTab = useRef(false);
  // True once the default view has been applied at least once. Prevents
  // subsequent storage updates (e.g. a fresh denial that would change
  // selectDefaultView from 'origins' to 'approvals') from yanking the
  // user off the tab they are currently on — the default applies on
  // first paint and on error recovery, never on a storage delta.
  const defaultViewApplied = useRef(false);
  // Distinguishes whether the default view was last set via the error
  // fallback, so the state-driven branch can re-apply selectDefaultView
  // once storage eventually delivers real state.
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
      // State is finally available — apply the default view exactly once
      // per clean first paint, or once more on recovery from an error
      // fallback. Subsequent storage updates re-render panel contents
      // but must not yank the user off the tab they are on.
      if (defaultedViaError.current) {
        if (!userPickedTab.current) {
          setActiveTab(selectDefaultView(state));
        }
        defaultViewApplied.current = true;
        defaultedViaError.current = false;
      } else if (!defaultViewApplied.current && !userPickedTab.current) {
        setActiveTab(selectDefaultView(state));
        defaultViewApplied.current = true;
      }
      return;
    }
    if (policyError !== null) {
      // No state available, but the initial read failed — fall back to
      // the Approvals tab so the panel is never blank. Mark this as an
      // error fallback so the state-driven branch above can re-apply the
      // default once storage eventually delivers real state.
      if (!userPickedTab.current) {
        setActiveTab('approvals');
      }
      defaultViewApplied.current = true;
      defaultedViaError.current = true;
    }
  }, [state, policyError]);

  useEffect(() => {
    void updateBadge().catch((error: unknown) =>
      setMessage(toErrorMessage(error)),
    );
  }, [setMessage]);

  // --- Takeover: the switch shows persisted state, not the click ---
  //
  // Not optimistic, and the value here is not this component's to decide. It
  // is whatever takeover-sync says the engine enforces, and takeover-sync
  // moves only on an observation or on a write it knows landed. The panel
  // used to keep the equivalent bookkeeping in three refs, which could only
  // be tested by reading this file and counting occurrences of a variable
  // name — a guard that passes when the behaviour is wrong. The logic is a
  // pure module now (takeover-sync.ts) so the cases are executed.
  const [takeover, setTakeover] = useState(false);
  const takeoverSync = useRef<TakeoverSync>(createTakeoverSync(false));
  const applySync = useCallback((next: TakeoverSync): void => {
    const prev = takeoverSync.current;
    takeoverSync.current = next;
    if (next.displayed !== prev.displayed) setTakeover(next.displayed);
  }, []);

  useEffect(() => {
    if (state === null) return;
    applySync(observe(takeoverSync.current, state.takeover));
  }, [state, applySync]);

  const handleTakeoverChange = useCallback(
    (desired: boolean): void => {
      const opening = click(takeoverSync.current, desired);
      const myGen = opening.gen;
      applySync(opening);

      void requestPolicyOp({ op: 'set_takeover', desired })
        .then(() => {
          applySync(settle(takeoverSync.current, myGen, true));
        })
        .catch((error: unknown) => {
          // A newer click superseded this one. Its error refers to an action
          // that no longer reflects the user's current intent, so no banner;
          // and the window it still holds must not be released here, or a
          // parked observation would apply against a write still in flight.
          if (takeoverSync.current.gen !== myGen) return;
          applySync(settle(takeoverSync.current, myGen, false));
          // The write failed, so the switch never moved and there is nothing
          // to revert: it still shows what the engine enforces. Just say why.
          setMessage(toErrorMessage(error));
        });
    },
    [applySync, setMessage],
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

  const dismissMessage = useCallback((): void => {
    // setMessage('') takes the explicit-clear path inside the helper —
    // it always wins, including against a persistent message, which is
    // the case the × button exists to handle.
    setMessage('');
  }, [setMessage]);

  // While the policy read is still in flight, the takeover prop renders
  // the hero's neutral 'Loading…' state. Once state has loaded at least
  // once (or the user has clicked during loading), we forward the local
  // takeover value so the switch keeps showing what it showed before the
  // re-render, and the sync effect can still update takeover once a
  // persisted state arrives.
  const takeoverLoaded = state !== null;
  const takeoverForHero =
    takeoverLoaded || takeoverSync.current.pending ? takeover : null;

  return (
    <div className={styles.app}>
      <div className={styles.topCluster}>
        <TakeoverHero
          takeover={takeoverForHero}
          browserConnected={browserConnected}
          browserId={browserId}
          paired={policy !== null && policy.pairingToken !== null}
          onTakeoverChange={handleTakeoverChange}
          onOpenSettings={handleOpenSettings}
        />
        {message !== '' && (
          <div className={styles.message} role="alert">
            <span className={styles.messageText}>{message}</span>
            <button
              type="button"
              className={styles.messageDismiss}
              aria-label="Dismiss message"
              onClick={dismissMessage}
            >
              ×
            </button>
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
        {activeTab !== null && (
          <>
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
          </>
        )}
      </div>
    </div>
  );
}

function panelClass(active: boolean): string {
  return active ? styles.panelActive : styles.panel;
}
