import styles from './GlassSwitch.module.css';

interface GlassSwitchProps {
  checked: boolean;
  onChange: (desired: boolean) => void;
  label: string;
  ariaLabel?: string;
  title?: string;
  engaged?: boolean;
}

// Tactile pill switch. `engaged` adds the design.md takeover glow: a
// red/amber breathing border highlight instead of the reference HTML's
// emerald glow (design.md wins on conflict).
export function GlassSwitch({
  checked,
  onChange,
  label,
  ariaLabel,
  title,
  engaged = false,
}: GlassSwitchProps) {
  return (
    <label className={styles.switch} title={title}>
      <span className={styles.label}>{label}</span>
      <input
        type="checkbox"
        className={styles.input}
        checked={checked}
        aria-label={ariaLabel ?? label}
        onChange={(event) => onChange(event.target.checked)}
      />
      <span
        className={`${styles.track}${checked ? ` ${styles.on}` : ''}${
          engaged && checked ? ` ${styles.engaged}` : ''
        }`}
        aria-hidden="true"
      >
        <span className={styles.thumb} />
      </span>
    </label>
  );
}
