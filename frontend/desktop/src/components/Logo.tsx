import styles from './Logo.module.css';
export function Logo({ size = 32, activity = 'idle' }: { size?: number; activity?: string }) {
  return <svg width={size} height={size} viewBox="0 0 40 40" fill="none" aria-hidden="true" className={styles.logo} data-activity={activity}>
    <path d="M9 27V19H18L22 24V28M16 20L28 11L30 14L20 24" fill="currentColor" />
    <rect x="4" y="27" width="23" height="9" rx="4.5" fill="currentColor" />
    <path d="M10 31.5H21" stroke="var(--surface)" strokeWidth="2" strokeLinecap="round" />
    <g className={styles.wheel} fill="var(--accent)">
      <path d="M27 3H32V7L35 5L38 9L35 12L39 13L38 18L34 17L34 21H29V17L25 20L22 16L25 13L21 12L22 7L26 8Z" />
      <circle cx="30" cy="12" r="3.5" fill="var(--surface)" /><circle cx="30" cy="12" r="1.5" />
    </g>
  </svg>;
}
