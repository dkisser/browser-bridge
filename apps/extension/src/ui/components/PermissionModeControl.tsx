import type { PermissionMode } from '@browser-bridge/shared';
import styles from './PermissionModeControl.module.css';

// The three-way Permission mode (ADR-0038), in ascending order of what runs
// silent. The order is the explanation: each step releases exactly one
// safety level's origin gating, and nothing else moves.
const MODES: { id: PermissionMode; label: string; hint: string }[] = [
  {
    id: 'strict',
    label: 'Strict',
    hint: 'Reading and writing both ask for origin approval.',
  },
  {
    id: 'standard',
    label: 'Standard',
    hint: 'Reads run within your approved origins; writes still ask.',
  },
  {
    id: 'relaxed',
    label: 'Relaxed',
    hint: 'Reads and writes run within your approved origins.',
  },
];

function hintFor(mode: PermissionMode): string {
  return MODES.find((entry) => entry.id === mode)?.hint ?? '';
}

interface PermissionModeControlProps {
  // null = the policy read is still in flight. The control renders with
  // nothing selected and disabled, for the same reason the hero renders
  // 'Loading…' rather than a wrong 'Agent Autonomous'.
  mode: PermissionMode | null;
  onChange: (mode: PermissionMode) => void;
}

// Human-only control (ADR-0038): the mode moves the threshold at which a
// command must clear the Working scope, so who sets it is part of the
// decision. It is rendered in the State bar and written through the
// side-panel → service-worker policy_op channel; no command, tool or MCP
// path can reach it.
//
// Deny, the blocklist, protected origins, unknown commands, sensitive actions
// and Takeover are unaffected by which option is selected — that boundary is
// why the copy below names what the mode does rather than what it permits.
//
// Native radio inputs, hidden and styled through their sibling track, the
// same idiom GlassSwitch uses for the Takeover toggle: the group is then one
// tab stop with the browser's own arrow-key movement, which is what the
// WAI-ARIA radio-group pattern asks for and what a roving-tabindex
// reimplementation has to earn back by hand.
export function PermissionModeControl({
  mode,
  onChange,
}: PermissionModeControlProps) {
  return (
    <div className={styles.control}>
      <span className={styles.eyebrow}>Permission mode</span>
      <div
        className={styles.track}
        role="radiogroup"
        aria-label="Permission mode"
      >
        {MODES.map((entry) => {
          const isActive = entry.id === mode;
          return (
            <label
              key={entry.id}
              className={styles.option}
              title={entry.hint}
              data-active={isActive ? 'true' : 'false'}
            >
              <input
                type="radio"
                name="permission-mode"
                className={styles.input}
                value={entry.id}
                checked={isActive}
                disabled={mode === null}
                onChange={() => onChange(entry.id)}
              />
              <span className={styles.optionLabel}>{entry.label}</span>
            </label>
          );
        })}
      </div>
      <p className={styles.hint}>
        {mode === null ? 'Loading…' : hintFor(mode)}
      </p>
    </div>
  );
}
