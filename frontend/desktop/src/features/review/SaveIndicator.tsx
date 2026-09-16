import { AlertCircle, Check, Circle, LoaderCircle, RotateCcw } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import type { SaveStatus } from './autosave';
import styles from './Review.module.css';

export function SaveIndicator({ status, retry }: { status: SaveStatus; retry: () => void }) {
  const { t } = useTranslation('review');
  const Icon = status === 'saving' ? LoaderCircle : status === 'failed' ? AlertCircle : status === 'saved' ? Check : Circle;
  return <div className={styles.saveIndicator} data-status={status} role="status" aria-live="polite">
    <Icon size={15} className={status === 'saving' ? styles.spin : undefined} /><span>{t(status)}</span>
    {status === 'failed' && <button type="button" className={styles.textButton} onClick={retry}><RotateCcw size={14} />{t('retry')}</button>}
  </div>;
}
