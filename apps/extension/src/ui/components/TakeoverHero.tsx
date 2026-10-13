import type { PermissionMode } from '@browser-bridge/shared';
import {
  type KeyboardEvent,
  type MouseEvent,
  useReducer,
  useState,
} from 'react';
import { confirmPairingCode } from '../bridge-api';
import { GearIcon, RefreshIcon } from '../icons';
import { GlassSwitch } from './GlassSwitch';
import { PermissionModeControl } from './PermissionModeControl';
import {
  initialPairDrawerState,
  pairDrawerReducer,
  shouldShowPairForm,
} from './pair-drawer';
import styles from './TakeoverHero.module.css';

interface TakeoverHeroProps {
  // null = state not yet loaded (rendering the OFF/ON label would flash
  // the wrong state on every panel open). The hero renders a neutral
  // "Loading…" state until the policy read resolves and SidePanel
  // supplies a real boolean.
  takeover: boolean | null;
  // Same null-means-unloaded contract for the Permission mode (ADR-0038).
  permissionMode: PermissionMode | null;
  browserConnected: boolean;
  browserId: string | null;
  paired: boolean;
  onTakeoverChange: (desired: boolean) => void;
  onPermissionModeChange: (mode: PermissionMode) => void;
  onOpenSettings: (event: MouseEvent<HTMLButtonElement>) => void;
}

export function TakeoverHero({
  takeover,
  permissionMode,
  browserConnected,
  browserId,
  paired,
  onTakeoverChange,
  onPermissionModeChange,
  onOpenSettings,
}: TakeoverHeroProps) {
  const [pairCode, setPairCode] = useState('');
  const [pairError, setPairError] = useState('');
  // Paired: the form stays collapsed behind the re-pair toggle; unpaired it
  // is always visible (that is the blocking state the user must resolve).
  const [drawer, dispatch] = useReducer(
    pairDrawerReducer,
    initialPairDrawerState,
  );
  const showPairForm = shouldShowPairForm(paired, drawer.open);

  const submitPairing = (): void => {
    const code = pairCode.trim();
    if (code === '') return;
    setPairError('');
    void confirmPairingCode(code).then((error) => {
      if (error === null) {
        setPairCode('');
        dispatch({ type: 'pairing-succeeded' });
      } else {
        setPairError(error);
      }
    });
  };

  const handlePairKeyDown = (event: KeyboardEvent<HTMLInputElement>): void => {
    if (event.key !== 'Enter') return;
    submitPairing();
  };

  return (
    <section className={styles.hero} data-purpose="takeover-hero-card">
      <div className={styles.topRow}>
        <div className={styles.state}>
          <span className={styles.eyebrow}>Takeover control</span>
          <div className={styles.stateLine}>
            <span
              className={`${styles.stateDot} ${
                takeover === null
                  ? styles.stateDotLoading
                  : takeover
                    ? styles.stateDotEngaged
                    : styles.stateDotIdle
              }`}
              aria-hidden="true"
            />
            <span className={styles.stateText}>
              {takeover === null
                ? 'Loading…'
                : takeover
                  ? 'Takeover active'
                  : 'Agent Autonomous'}
            </span>
          </div>
        </div>
        <GlassSwitch
          checked={takeover === true}
          onChange={onTakeoverChange}
          label="Takeover"
          ariaLabel="Human Takeover"
          title="Takeover: reject agent commands while you drive"
          engaged
        />
      </div>

      <PermissionModeControl
        mode={permissionMode}
        onChange={onPermissionModeChange}
      />

      <div className={styles.connectionRow}>
        <span className={styles.dotWrap} aria-hidden="true">
          {browserConnected && <span className={styles.ping} />}
          <span
            className={`${styles.dot} ${
              browserConnected ? styles.dotConnected : styles.dotDisconnected
            }`}
          />
        </span>
        <span className={styles.connectionLabel}>Browser</span>
        <span
          className={`${styles.connectionState} ${
            browserConnected ? styles.connected : styles.disconnected
          }`}
        >
          {browserConnected ? 'Connected' : 'Disconnected'}
        </span>
        <span className={styles.spacer} />
        {browserId !== null && (
          <span className={styles.uid} title={browserId}>
            {browserId}
          </span>
        )}
        <button
          type="button"
          className={styles.iconButton}
          onClick={onOpenSettings}
          aria-label="Settings"
          title="Settings"
        >
          <GearIcon size={13} />
        </button>
        {paired && (
          <button
            type="button"
            className={
              drawer.open ? styles.iconButtonActive : styles.iconButton
            }
            onClick={() => dispatch({ type: 'toggle' })}
            aria-label="Re-pair"
            title="Re-pair with bridge-core"
          >
            <RefreshIcon size={13} />
          </button>
        )}
      </div>

      {showPairForm && (
        <div className={styles.pairingDrawer}>
          <div className={styles.pairForm}>
            <div className={styles.pairTitle}>Pair with bridge-core</div>
            <p className={styles.pairHint}>
              Run <code>bridge pair</code> in a terminal to get a code.
            </p>
            <div className={styles.pairRow}>
              <input
                type="text"
                value={pairCode}
                placeholder="Pairing code"
                onChange={(event) => setPairCode(event.target.value)}
                onKeyDown={handlePairKeyDown}
              />
              <button
                type="button"
                className={styles.pairButton}
                onClick={submitPairing}
              >
                Pair
              </button>
            </div>
            {pairError !== '' && (
              <div className={styles.pairError}>{pairError}</div>
            )}
          </div>
        </div>
      )}
    </section>
  );
}
