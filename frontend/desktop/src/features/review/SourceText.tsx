import { Fragment, useEffect, useMemo, useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { AlertCircle, Check, ChevronDown, ChevronUp, Copy, FileText, LoaderCircle, Search } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { api } from '../../lib/api';
import type { Source } from './types';
import styles from './Review.module.css';

/** Literal ranges, so punctuation and regex syntax in search are ordinary text. */
export function findTextMatches(text: string, query: string): { start: number; end: number }[] {
  if (!query) return [];
  const needle = query.toLocaleLowerCase();
  const haystack = text.toLocaleLowerCase();
  const matches = [];
  let from = 0;
  while (from < haystack.length) {
    const start = haystack.indexOf(needle, from);
    if (start === -1) break;
    matches.push({ start, end: start + query.length });
    from = start + query.length;
  }
  return matches;
}

export function SourceText({ conspectID }: { conspectID?: string }) {
  const { t, i18n } = useTranslation('review');
  const params = new URLSearchParams(window.location.search);
  const id = conspectID || params.get('conspect') || params.get('conspectID') || params.get('id') || '';
  const [search, setSearch] = useState('');
  const [active, setActive] = useState(0);
  const [copyStatus, setCopyStatus] = useState<'idle' | 'copied' | 'failed'>('idle');
  const content = useRef<HTMLDivElement>(null);
  const query = useQuery({ queryKey: ['desktop', 'source-text', id], queryFn: () => api<Source>(`/api/v1/review/conspects/${encodeURIComponent(id)}/source-text`), enabled: !!id });
  const source = query.data;
  const matches = useMemo(() => findTextMatches(source?.text || '', search), [source?.text, search]);
  useEffect(() => { setActive(0); }, [search, id]);
  useEffect(() => { content.current?.querySelector<HTMLElement>('[data-active="true"]')?.scrollIntoView({ block: 'center', behavior: 'instant' }); }, [active, matches]);
  useEffect(() => {
    if (copyStatus !== 'copied') return;
    const timer = setTimeout(() => setCopyStatus('idle'), 2500);
    return () => clearTimeout(timer);
  }, [copyStatus]);
  function navigate(direction: number) { if (matches.length) setActive(value => (value + direction + matches.length) % matches.length); }
  async function copy() {
    try { await navigator.clipboard.writeText(source?.text || ''); setCopyStatus('copied'); }
    catch { setCopyStatus('failed'); }
  }
  const formatter = new Intl.DateTimeFormat(i18n.language, { dateStyle: 'medium', timeStyle: 'short' });
  const formatTime = (value?: string) => value && !Number.isNaN(Date.parse(value)) ? formatter.format(new Date(value)) : '';
  return <main className={styles.sourceWindow}>
    <header className={styles.sourceHeader}><div><span className={styles.eyebrow}><FileText size={15} />KnowledgeCrawler</span><h1>{t('sourceText')}</h1>{source && <p className={styles.metadata}>{t(`source.${source.source_kind.split('/').at(-1)}`, { defaultValue: t('source.unknown') })}<span>·</span>{formatTime(source.started_at)}{source.ended_at !== source.started_at && ` — ${formatTime(source.ended_at)}`}</p>}</div><button type="button" className={styles.secondaryButton} disabled={!source?.text} onClick={() => { void copy(); }}>{copyStatus === 'copied' ? <Check size={17} /> : <Copy size={17} />}{t(copyStatus === 'copied' ? 'copied' : 'copy')}</button></header>
    <div className={styles.sourceToolbar}><div className={styles.searchField}><Search size={17} /><input type="search" aria-label={t('searchText')} placeholder={t('searchText')} value={search} onChange={event => setSearch(event.target.value)} onKeyDown={event => { if (event.key === 'Enter') { event.preventDefault(); navigate(event.shiftKey ? -1 : 1); } }} /></div><span aria-live="polite" className={styles.matchCount}>{search ? t('matches', { count: matches.length }) : ''}</span><button type="button" className={styles.iconButton} disabled={!matches.length} aria-label={t('previousMatch')} onClick={() => navigate(-1)}><ChevronUp size={18} /></button><button type="button" className={styles.iconButton} disabled={!matches.length} aria-label={t('nextMatch')} onClick={() => navigate(1)}><ChevronDown size={18} /></button></div>
    {copyStatus === 'failed' && <p className={styles.inlineError} role="alert">{t('copyFailed')}</p>}
    <div className={styles.sourceContent} ref={content} tabIndex={0} aria-label={t('sourceText')}>
      {!id ? <div className={styles.empty}><FileText size={30} /><p>{t('sourceMissing')}</p></div> : query.isPending ? <div className={styles.empty}><LoaderCircle size={28} className={styles.spin} /><p>{t('sourceLoading')}</p></div> : query.isError ? <div className={styles.empty} role="alert"><AlertCircle size={28} /><p>{t('sourceFailed')}</p><button type="button" className={styles.secondaryButton} onClick={() => { void query.refetch(); }}>{t('retry')}</button></div> : <article className={styles.transcript}>{!source?.text ? t('noText') : matches.length ? <>{matches.map((match, index) => <Fragment key={match.start}>{source.text.slice(index ? matches[index - 1].end : 0, match.start)}<mark data-active={index === active}>{source.text.slice(match.start, match.end)}</mark></Fragment>)}{source.text.slice(matches.at(-1)!.end)}</> : source.text}</article>}
    </div>
  </main>;
}
