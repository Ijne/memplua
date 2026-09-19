import { useEffect, useRef, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { AudioLines, BrainCircuit, CircleAlert, GripVertical, Inbox, LoaderCircle, Mic, MicOff, Network, Pause, Play, Settings, ShieldCheck, Square, VolumeX, X } from 'lucide-react';
import { Logo } from '../../components/Logo';
import { api } from '../../lib/api';
import { openWindow, quitApp, resizeWidget } from '../../lib/bridge';
import type { UISource, UIState } from '../../lib/types';
import styles from './Widget.module.css';

const CLOSED_WIDTH = 409;
const WIDGET_HEIGHT = 48;

function elapsed(source: UISource, now: number) {
  if (!source.started_at) return '0:00';
  const started = Date.parse(source.started_at);
  const ended = source.state === 'paused' && source.stopped_at ? Date.parse(source.stopped_at) : now;
  const seconds = Math.max(0, Math.floor((ended - started) / 1000));
  const hours = Math.floor(seconds / 3600);
  const minutes = Math.floor(seconds % 3600 / 60);
  const rest = seconds % 60;
  return hours ? `${hours}:${String(minutes).padStart(2, '0')}:${String(rest).padStart(2, '0')}` : `${minutes}:${String(rest).padStart(2, '0')}`;
}

export function Widget({ connected = true }: { connected?: boolean }) {
  const { t } = useTranslation();
  const client = useQueryClient();
  const state = useQuery({ queryKey: ['ui-state'], queryFn: ({ signal }) => api<UIState>('/api/v1/ui/state', { signal }), refetchInterval: 10_000 });
  const [now, setNow] = useState(Date.now());
  const [windowError, setWindowError] = useState(false);
  const [expandedSource, setExpandedSource] = useState<string | null>(null);
  const resizedWidth = useRef(CLOSED_WIDTH);
  const data = state.data;
  const mutation = useMutation({
    mutationFn: ({ source, action }: { source: UISource; action: string }) => api(action === 'start' ? `/api/v1/sources/${encodeURIComponent(source.id)}/start` : `/api/v1/source-sessions/${encodeURIComponent(source.session_id || '')}/${action}`, { method: 'POST' }),
    onSuccess: () => client.invalidateQueries({ queryKey: ['ui-state'] }),
  });
  const showWindow = (name: 'review' | 'settings' | 'graph') => { setWindowError(false); void openWindow(name).catch(() => setWindowError(true)); };
  useEffect(() => {
    if (!data?.sources.some(source => source.state === 'recording')) return;
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, [data?.sources]);
  const sources = ['microphone', 'loopback'].map(kind => data?.sources.find(source => source.id === kind || source.kind === kind));
  const expanded = sources.find((source, index) => expandedSource === (index ? 'loopback' : 'microphone') && source?.actions.some(action => ['start', 'pause', 'resume', 'stop'].includes(action)));
  const widgetWidth = CLOSED_WIDTH + (expanded ? ['recording', 'paused'].includes(expanded.state) ? 112 : 36 : 0);
  useEffect(() => {
    const shrinking = widgetWidth < resizedWidth.current;
    resizedWidth.current = widgetWidth;
    const apply = () => { void resizeWidget(widgetWidth, WIDGET_HEIGHT).catch(() => setWindowError(true)); };
    if (!shrinking) { apply(); return; }
    const timer = window.setTimeout(apply, 190);
    return () => window.clearTimeout(timer);
  }, [widgetWidth]);
  const activity = data?.processing === 'working' ? 'working' : sources.some(source => source?.state === 'recording') ? 'recording' : 'idle';
  const processing = data?.processing || 'idle';
  const failure = Boolean(state.isError || !connected || windowError);
  const healthy = Boolean(data && !failure && !data.warning);
  const healthLabel = !data && state.isPending ? t('loading') : failure ? t('reconnecting') : data?.warning ? t('attention') : t('noWarnings');

  return <div className={styles.root} style={{ width: widgetWidth }}>
    <div className={styles.dock} role="toolbar" aria-label={t('widgetLabel')}>
      <div className={styles.dragHandle} title={t('dragWidget')} aria-hidden="true"><GripVertical /></div>
      <div className={styles.brand} title="memplua"><Logo size={29} activity={activity} /></div>
      <div className={styles.separator} />
      {sources.map((source, index) => {
        const kind = index ? 'loopback' : 'microphone';
        const status = source?.state || 'off';
        const Icon = status === 'starting' ? LoaderCircle : status === 'failed' ? CircleAlert : status === 'paused' ? Pause : index ? (status === 'off' ? VolumeX : AudioLines) : (status === 'off' ? MicOff : Mic);
        const actions = source?.actions.filter(action => ['start', 'pause', 'resume', 'stop'].includes(action)) || [];
        const isExpanded = expandedSource === kind && !!actions.length;
        return <div className={styles.sourceControl} data-state={status} data-expanded={isExpanded || undefined} key={kind}
          onPointerEnter={() => setExpandedSource(kind)} onPointerLeave={event => { if (!event.currentTarget.contains(document.activeElement)) setExpandedSource(null); }}
          onFocus={() => setExpandedSource(kind)} onBlur={event => { if (!event.currentTarget.contains(event.relatedTarget as Node | null)) setExpandedSource(null); }}>
          <button type="button" className={styles.control} data-state={status} aria-label={`${t(kind)}: ${t(`sourceState.${status}`)}`} title={`${t(kind)} — ${t(`sourceState.${status}`)}`} disabled={!data || !source}>
            <Icon className={status === 'starting' || mutation.isPending && mutation.variables?.source.id === source?.id ? 'spin' : ''} />{status === 'recording' && <span className={styles.dot} />}
          </button>
          {!!actions.length && <div className={styles.sourceActions} aria-label={t(kind)}>
            {(status === 'recording' || status === 'paused') && <time dateTime={source?.started_at}>{source ? elapsed(source, now) : '0:00'}</time>}
            {actions.map(action => <button type="button" key={action} disabled={!source || mutation.isPending || (action === 'start' && !source.available)} aria-label={t(action)} title={t(action)} onPointerUp={event => event.currentTarget.blur()} onClick={() => source && mutation.mutate({ source, action })}>
              {action === 'stop' ? <Square /> : action === 'pause' ? <Pause /> : <Play />}
            </button>)}
          </div>}
        </div>;
      })}
      <button type="button" className={styles.control} data-state={processing} title={`${t('processing')} — ${t(`processState.${processing}`)}`} aria-label={t(`processState.${processing}`)}><BrainCircuit className={processing === 'working' ? styles.thinking : ''} /></button>
      <div className={styles.separator} />
      <button type="button" className={styles.inbox} title={t('inboxTooltip', { conspects: data?.pending_conspects || 0, items: data?.pending_items || 0 })} aria-label={`${t('review')}: ${data?.pending_conspects || 0}`} onClick={() => showWindow('review')}><Inbox /><span>{data?.pending_conspects || 0}</span></button>
      <button type="button" className={styles.control} aria-label={t('graph')} title={t('graph')} onClick={() => showWindow('graph')}><Network /></button>
      <button type="button" className={`${styles.control} ${healthy ? styles.healthy : ''}`} data-state={data?.warning || failure ? 'failed' : healthy ? 'healthy' : 'starting'} title={healthLabel} aria-label={healthLabel}>{!data && state.isPending ? <LoaderCircle className="spin" /> : failure || data?.warning ? <CircleAlert /> : <ShieldCheck />}</button>
      <button type="button" className={styles.control} aria-label={t('settings')} title={t('settings')} onClick={() => showWindow('settings')}><Settings /></button>
      <button type="button" className={`${styles.control} ${styles.quit}`} aria-label={t('quit')} title={t('quit')} onClick={() => void quitApp()}><X /></button>
    </div>
  </div>;
}
