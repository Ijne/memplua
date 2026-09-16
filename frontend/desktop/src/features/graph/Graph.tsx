import { useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { BookOpen, ChevronRight, GitFork, Lightbulb, LockKeyhole, Network, Search } from 'lucide-react';
import { api } from '../../lib/api';
import { openWindow } from '../../lib/bridge';
import { Failure, Loading } from '../../components/Feedback';
import styles from './Graph.module.css';
interface Term { id: string; name: string; description: string; tags: string[] }
interface Thought { id: string; thesis: string; body: string; tags: string[] }
interface Snapshot { terms: Term[]; thoughts: Thought[]; links: { term_id: string; thought_id: string }[] }
export function Graph() {
  const { t } = useTranslation();
  const [search, setSearch] = useState(''), [kind, setKind] = useState('all'), [selected, setSelected] = useState('');
  const query = useQuery({ queryKey: ['graph'], queryFn: ({ signal }) => api<Snapshot>('/api/v1/graph', { signal }) });
  const entries = useMemo(() => [...(query.data?.terms || []).map((term) => ({ id: term.id, title: term.name, text: term.description, tags: term.tags || [], kind: 'terms' })), ...(query.data?.thoughts || []).map((thought) => ({ id: thought.id, title: thought.thesis, text: thought.body, tags: thought.tags || [], kind: 'thoughts' }))], [query.data]);
  const filtered = entries.filter((entry) => (kind === 'all' || entry.kind === kind) && `${entry.title} ${entry.text} ${entry.tags.join(' ')}`.toLocaleLowerCase().includes(search.toLocaleLowerCase()));
  const current = filtered.find((entry) => entry.id === selected) || filtered[0];
  const related = current && (query.data?.links || []).filter((link) => link.term_id === current.id || link.thought_id === current.id).map((link) => entries.find((entry) => entry.id === (current.kind === 'terms' ? link.thought_id : link.term_id))).filter((entry) => Boolean(entry));
  if (!query.data) return query.isError ? <Failure retry={() => void query.refetch()} /> : <Loading />;
  return <main className={styles.page}>
    <header className={styles.header}><div><div className={styles.eyebrow}><Network /> KnowledgeCrawler</div><h1>{t('graph')}</h1><p>{t('graphIntro')}</p></div><span className={styles.readonly}><LockKeyhole />{t('readOnly')}</span></header>
    <div className={styles.stats}>{[[BookOpen, 'terms', query.data.terms?.length || 0], [Lightbulb, 'thoughts', query.data.thoughts?.length || 0], [GitFork, 'connections', query.data.links?.length || 0]].map(([Icon, label, count]) => { const StatIcon = Icon as typeof BookOpen; return <div key={String(label)}><StatIcon /><b>{Number(count)}</b><span>{t(String(label))}</span></div>; })}</div>
    {!entries.length ? <div className={styles.empty}><Network /><h2>{t('graphEmpty')}</h2><p>{t('graphEmptyHint')}</p><button className="primary" onClick={() => void openWindow('review')}>{t('review')}<ChevronRight /></button></div> : <>
      <div className={styles.toolbar}><label className={styles.search}><Search /><input type="search" placeholder={t('graphSearch')} aria-label={t('graphSearch')} value={search} onChange={(event) => setSearch(event.target.value)} /></label><div className={styles.filters} role="group" aria-label={t('graph')}>{['all', 'terms', 'thoughts'].map((value) => <button key={value} aria-pressed={kind === value} onClick={() => setKind(value)}>{t(value)}</button>)}</div></div>
      <div className={styles.workspace}><nav className={styles.list} aria-label={t('graph')}>{filtered.map((entry) => <button key={entry.id} onClick={() => setSelected(entry.id)} aria-current={entry.id === current?.id ? 'true' : undefined}>{entry.kind === 'terms' ? <BookOpen /> : <Lightbulb />}<span><strong>{entry.title}</strong><small>{t(entry.kind)}</small></span><ChevronRight /></button>)}{!filtered.length && <p className={styles.noResults}>{t('noResults')}</p>}</nav>
        <article className={styles.detail}>{current && <><span className={styles.kind}>{current.kind === 'terms' ? <BookOpen /> : <Lightbulb />}{t(current.kind)}</span><h2>{current.title}</h2><p className={styles.content}>{current.text}</p><div className={styles.tags}>{current.tags.map((tag) => <span key={tag}>{tag}</span>)}</div>{Boolean(related?.length) && <section className={styles.related}><h3>{t('connections')}</h3>{related?.map((entry) => entry && <button key={entry.id} onClick={() => { setSearch(''); setKind('all'); setSelected(entry.id); }}>{entry.kind === 'terms' ? <BookOpen /> : <Lightbulb />}{entry.title}<ChevronRight /></button>)}</section>}</>}</article>
      </div>
    </>}
  </main>;
}
