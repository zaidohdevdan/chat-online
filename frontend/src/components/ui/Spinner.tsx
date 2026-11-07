import styles from './Spinner.module.css';

export interface SpinnerProps {
  label?: string;
}

export function Spinner({ label }: SpinnerProps) {
  return (
    <span role="status" aria-live="polite" className={styles.spinner}>
      {label ? <span className="srOnly">{label}</span> : null}
    </span>
  );
}

