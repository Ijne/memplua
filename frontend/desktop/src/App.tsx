import { Component, lazy, Suspense, useEffect, useState, type ReactNode } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { CircleAlert, LockKeyhole } from 'lucide-react';
import { api, ApiError, configureAPI } from './lib/api';
import { bootstrap, isDesktop, type WindowName } from './lib/bridge';
import { useLiveEvents } from './lib/events';
import { applyTheme, usePreferences } from './lib/preferences';
import { Logo } from './components/Logo';
import { Failure, Loading } from './components/Feedback';
import { Widget } from './features/widget/Widget';
import i18n from './lib/i18n';
import styles from './App.module.css';

const Review = lazy(() => import('./features/review/Review').then((module) => ({ default: module.Review })));
const SourceText = lazy(() => import('./features/review/SourceText').then((module) => ({ default: module.SourceText })));
const Settings = lazy(() => import('./features/settings/Settings').then((module) => ({ default: module.Settings })));
const Graph = lazy(() => import('./features/graph/Graph').then((module) => ({ default: module.Graph })));
export const queryClient = new QueryClient({ defaultOptions: { queries: { retry: (count, error) => !(error instanceof ApiError && error.status < 500) && count < 2, staleTime: 5000, refetchOnWindowFocus: true }, mutations: { retry: false } } });

class ErrorBoundary extends Component<{ children: ReactNode }, { failed: boolean }> {
  state = { failed: false };
  static getDerivedStateFromError() { return { failed: true }; }
  render() { return this.state.failed ? <div className={styles.launch}><CircleAlert /><h1>{i18n.t('fatalTitle')}</h1><p>{i18n.t('fatalHint')}</p><button onClick={() => location.reload()}>{i18n.t('reload')}</button></div> : this.props.children; }
}
function ConnectedApp() {
  const { t } = useTranslation();
  usePreferences();
  const connected = useLiveEvents();
  const params = new URLSearchParams(location.search);
  const name = (params.get('window') || 'review') as WindowName;
  useEffect(() => {
    document.documentElement.dataset.window = name;
    document.title = `${t(name)} — KnowledgeCrawler`;
    return () => { delete document.documentElement.dataset.window; };
  }, [name, t]);
  if (name === 'widget') return <Widget connected={connected} />;
  return <Suspense fallback={<Loading />}>{name === 'settings' ? <Settings /> : name === 'source' ? <SourceText /> : name === 'graph' ? <Graph /> : <Review />}</Suspense>;
}
export function App() {
  const { t } = useTranslation();
  const [status, setStatus] = useState<'loading' | 'ready' | 'preview' | 'failed'>('loading');
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    let active = true;
    applyTheme('system');
    async function connect() {
      try {
        if (!isDesktop()) { if (active) setStatus('preview'); return; }
        const connection = await bootstrap();
        configureAPI(connection.apiAddress, connection.token);
        await i18n.changeLanguage(connection.locale.toLowerCase().startsWith('ru') ? 'ru' : 'en');
        await api('/api/v1/ui/state');
        if (active) setStatus('ready');
      } catch { if (active) setStatus('failed'); }
    }
    void connect(); return () => { active = false; };
  }, [attempt]);
  return <ErrorBoundary><QueryClientProvider client={queryClient}>{status === 'ready' ? <ConnectedApp /> : status === 'loading' ? <Loading /> : status === 'failed' ? <Failure retry={() => { setStatus('loading'); setAttempt((value) => value + 1); }} /> : <div className={styles.launch}><Logo size={96} /><span className={styles.brand}>KnowledgeCrawler</span><h1>{t('launchTitle')}</h1><p>{t('launchHint')}</p><span className={styles.privacy}><LockKeyhole />{t('local')}</span></div>}</QueryClientProvider></ErrorBoundary>;
}
