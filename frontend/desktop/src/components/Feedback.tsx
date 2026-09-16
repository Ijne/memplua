import { CircleAlert, LoaderCircle, RefreshCw } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import styles from './Feedback.module.css';
export function Loading() { const { t } = useTranslation(); return <div className={styles.center} role="status"><LoaderCircle className="spin" /><span>{t('loading')}</span></div>; }
export function Failure({ retry }: { retry?: () => void }) { const { t } = useTranslation(); return <div className={styles.center} role="alert"><CircleAlert /><h2>{t('connectionError')}</h2><p>{t('connectionHint')}</p>{retry && <button onClick={retry}><RefreshCw />{t('retry')}</button>}</div>; }
export function InlineError({ children }: { children?: React.ReactNode }) { const { t } = useTranslation(); return <div className={styles.error} role="alert"><CircleAlert /><span>{children || t('actionError')}</span></div>; }
