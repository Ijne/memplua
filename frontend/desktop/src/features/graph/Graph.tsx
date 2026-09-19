import { useMemo, useRef, useState, type CSSProperties, type KeyboardEvent, type PointerEvent, type WheelEvent } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import {
  BookOpen, ChevronRight, CircleDot, Focus, GitFork, Layers3, Lightbulb,
  LockKeyhole, Minus, Network, Plus, Search, Tag,
} from 'lucide-react';
import { api } from '../../lib/api';
import { openWindow } from '../../lib/bridge';
import { Failure, Loading } from '../../components/Feedback';
import styles from './Graph.module.css';

interface Term { id: string; name: string; description: string; tags: string[] }
interface Thought { id: string; thesis: string; body: string; tags: string[] }
interface Link { id?: string; term_id: string; thought_id: string }
interface Snapshot { revision: number; terms: Term[]; thoughts: Thought[]; links: Link[] }
type NodeKind = 'term' | 'thought';
type ViewMode = 'terms' | 'expanded';
interface Entry { id: string; title: string; text: string; tags: string[]; kind: NodeKind }
interface GraphNode extends Entry { x: number; y: number; cluster: string; color: number; order: number }
interface GraphEdge { id: string; source: string; target: string }
interface Cluster { name: string; x: number; y: number; rx: number; ry: number; color: number }

const WIDTH = 1000;
const HEIGHT = 640;
const PALETTE_SIZE = 7;
const UNTAGGED = '__untagged__';
const goldenAngle = Math.PI * (3 - Math.sqrt(5));
const clamp = (value: number, minimum: number, maximum: number) => Math.min(maximum, Math.max(minimum, value));
const normalize = (value: string) => value.trim().toLocaleLowerCase();
const shortTitle = (value: string) => value.length > 27 ? `${value.slice(0, 26)}…` : value;

function buildEntries(snapshot: Snapshot): Entry[] {
  return [
    ...snapshot.terms.map((term) => ({ id: term.id, title: term.name, text: term.description, tags: term.tags || [], kind: 'term' as const })),
    ...snapshot.thoughts.map((thought) => ({ id: thought.id, title: thought.thesis, text: thought.body, tags: thought.tags || [], kind: 'thought' as const })),
  ];
}

function buildEdges(snapshot: Snapshot, mode: ViewMode, visible: Set<string>): GraphEdge[] {
  if (mode === 'expanded') {
    return snapshot.links
      .filter((link) => visible.has(link.term_id) && visible.has(link.thought_id))
      .map((link) => ({ id: link.id || `${link.term_id}:${link.thought_id}`, source: link.term_id, target: link.thought_id }));
  }
  const termsByThought = new Map<string, string[]>();
  for (const link of snapshot.links) {
    if (!visible.has(link.term_id)) continue;
    const terms = termsByThought.get(link.thought_id) || [];
    terms.push(link.term_id);
    termsByThought.set(link.thought_id, terms);
  }
  const edges = new Map<string, GraphEdge>();
  for (const terms of termsByThought.values()) {
    const unique = [...new Set(terms)].sort();
    for (let left = 0; left < unique.length; left += 1) for (let right = left + 1; right < unique.length; right += 1) {
      const id = `${unique[left]}:${unique[right]}`;
      edges.set(id, { id, source: unique[left], target: unique[right] });
    }
  }
  return [...edges.values()];
}

function layout(entries: Entry[], tagOrder: string[]): { nodes: GraphNode[]; clusters: Cluster[] } {
  const grouped = new Map<string, Entry[]>();
  for (const entry of entries) {
    const cluster = [...entry.tags].sort((left, right) => {
      const leftOrder = tagOrder.indexOf(left), rightOrder = tagOrder.indexOf(right);
      if (leftOrder < 0) return 1;
      if (rightOrder < 0) return -1;
      return leftOrder - rightOrder;
    })[0] || UNTAGGED;
    grouped.set(cluster, [...(grouped.get(cluster) || []), entry]);
  }
  const names = [...grouped.keys()].sort((left, right) => {
    const leftOrder = tagOrder.indexOf(left), rightOrder = tagOrder.indexOf(right);
    if (leftOrder >= 0 && rightOrder >= 0 && leftOrder !== rightOrder) return leftOrder - rightOrder;
    return left.localeCompare(right);
  });
  const centers = names.map((name, index) => {
    if (names.length === 1) return { name, x: WIDTH / 2, y: HEIGHT / 2 };
    const columns = names.length <= 4 ? 2 : 3;
    const rows = Math.ceil(names.length / columns);
    const column = index % columns;
    const row = Math.floor(index / columns);
    return {
      name,
      x: 235 + column * (530 / (columns - 1)),
      y: rows === 1 ? HEIGHT / 2 : 185 + row * (280 / Math.max(rows - 1, 1)),
    };
  });
  const nodes: GraphNode[] = [];
  const clusters: Cluster[] = [];
  centers.forEach((center, color) => {
    const members = (grouped.get(center.name) || []).sort((left, right) => left.kind.localeCompare(right.kind) || left.title.localeCompare(right.title));
    const spread = Math.max(62, 35 * Math.sqrt(members.length));
    clusters.push({ ...center, rx: spread + 72, ry: spread * .72 + 58, color: color % PALETTE_SIZE });
    members.forEach((entry, index) => {
      const pairOffset = members.length === 2 ? (index === 0 ? -70 : 70) : 0;
      const radius = members.length <= 1 ? 0 : members.length <= 4 ? 74 : 52 * Math.sqrt(index);
      const angle = members.length <= 4 ? index * ((Math.PI * 2) / members.length) - Math.PI / 2 : index * goldenAngle - Math.PI / 2;
      nodes.push({
        ...entry,
        cluster: center.name,
        color: color % PALETTE_SIZE,
        order: nodes.length,
        x: center.x + (members.length === 2 ? pairOffset : Math.cos(angle) * radius),
        y: center.y + (members.length === 2 ? 0 : Math.sin(angle) * radius * .72),
      });
    });
  });
  return { nodes, clusters };
}

function curve(source: GraphNode, target: GraphNode) {
  const middleX = (source.x + target.x) / 2;
  const middleY = (source.y + target.y) / 2;
  const offset = Math.min(28, Math.hypot(target.x - source.x, target.y - source.y) * .08);
  return `M ${source.x} ${source.y} Q ${middleX} ${middleY - offset} ${target.x} ${target.y}`;
}

export function Graph() {
  const { t } = useTranslation();
  const [search, setSearch] = useState('');
  const [mode, setMode] = useState<ViewMode>('terms');
  const [activeTag, setActiveTag] = useState('');
  const [selected, setSelected] = useState('');
  const [transform, setTransform] = useState({ x: 0, y: 0, scale: 1 });
  const pan = useRef<{ x: number; y: number; originX: number; originY: number } | null>(null);
  const query = useQuery({ queryKey: ['graph'], queryFn: ({ signal }) => api<Snapshot>('/api/v1/graph', { signal }) });
  const entries = useMemo(() => query.data ? buildEntries(query.data) : [], [query.data]);
  const tags = useMemo(() => {
    const counts = new Map<string, { terms: number; thoughts: number }>();
    for (const entry of entries) for (const tag of entry.tags) {
      const count = counts.get(tag) || { terms: 0, thoughts: 0 };
      count[entry.kind === 'term' ? 'terms' : 'thoughts'] += 1;
      counts.set(tag, count);
    }
    return [...counts.entries()].map(([name, counts]) => ({ name, ...counts, total: counts.terms + counts.thoughts }))
      .sort((left, right) => right.total - left.total || left.name.localeCompare(right.name));
  }, [entries]);
  const shownEntries = useMemo(() => entries.filter((entry) =>
    (mode === 'expanded' || entry.kind === 'term') && (!activeTag || entry.tags.includes(activeTag))), [entries, mode, activeTag]);
  const visibleIds = useMemo(() => new Set(shownEntries.map((entry) => entry.id)), [shownEntries]);
  const graph = useMemo(() => layout(shownEntries, tags.map((tag) => tag.name)), [shownEntries, tags]);
  const edges = useMemo(() => query.data ? buildEdges(query.data, mode, visibleIds) : [], [query.data, mode, visibleIds]);
  const nodesByID = useMemo(() => new Map(graph.nodes.map((node) => [node.id, node])), [graph.nodes]);
  const normalizedSearch = normalize(search);
  const matches = useMemo(() => new Set(entries.filter((entry) => !normalizedSearch || normalize(`${entry.title} ${entry.text} ${entry.tags.join(' ')}`).includes(normalizedSearch)).map((entry) => entry.id)), [entries, normalizedSearch]);
  const listEntries = entries.filter((entry) => (!activeTag || entry.tags.includes(activeTag)) && matches.has(entry.id));
  const current = entries.find((entry) => entry.id === selected);
  const related = current && query.data ? query.data.links
    .filter((link) => link.term_id === current.id || link.thought_id === current.id)
    .map((link) => entries.find((entry) => entry.id === (current.kind === 'term' ? link.thought_id : link.term_id)))
    .filter((entry): entry is Entry => Boolean(entry)) : [];

  if (!query.data) return query.isError ? <Failure retry={() => void query.refetch()} /> : <Loading />;

  const setGraphMode = (next: ViewMode) => {
    setMode(next);
    setTransform({ x: 0, y: 0, scale: 1 });
    if (next === 'terms' && current?.kind === 'thought') setSelected('');
  };
  const pick = (entry: Entry) => {
    if (entry.kind === 'thought' && mode === 'terms') setMode('expanded');
    setSelected(entry.id);
  };
  const zoom = (delta: number) => setTransform((value) => ({ ...value, scale: clamp(value.scale + delta, .65, 2.2) }));
  const onWheel = (event: WheelEvent<SVGSVGElement>) => {
    event.preventDefault();
    zoom(event.deltaY > 0 ? -.1 : .1);
  };
  const onPointerDown = (event: PointerEvent<SVGSVGElement>) => {
    if ((event.target as Element).closest('[data-node]')) return;
    event.currentTarget.setPointerCapture(event.pointerId);
    pan.current = { x: event.clientX, y: event.clientY, originX: transform.x, originY: transform.y };
  };
  const onPointerMove = (event: PointerEvent<SVGSVGElement>) => {
    if (!pan.current) return;
    setTransform((value) => ({ ...value, x: pan.current!.originX + (event.clientX - pan.current!.x), y: pan.current!.originY + (event.clientY - pan.current!.y) }));
  };
  const stopPan = () => { pan.current = null; };
  const nodeKey = (event: KeyboardEvent<SVGGElement>, node: GraphNode) => {
    if (event.key === 'Enter' || event.key === ' ') { event.preventDefault(); pick(node); }
  };

  return <main className={styles.page}>
    <header className={styles.header}>
      <div><div className={styles.eyebrow}><CircleDot />memplua · {t('knowledgeMap')}</div><h1>{t('graph')}</h1><p>{t('graphIntro')}</p></div>
      <div className={styles.headerMeta}><span><span className={styles.liveDot} />{t('graphSynced')}</span><span><LockKeyhole />{t('readOnly')}</span></div>
    </header>

    {!entries.length ? <div className={styles.empty}><Network /><h2>{t('graphEmpty')}</h2><p>{t('graphEmptyHint')}</p><button className="primary" onClick={() => void openWindow('review')}>{t('review')}<ChevronRight /></button></div> :
      <section className={styles.workspace}>
        <aside className={styles.sidebar}>
          <label className={styles.search}><Search /><input type="search" placeholder={t('graphSearch')} aria-label={t('graphSearch')} value={search} onChange={(event) => setSearch(event.target.value)} /></label>
          <div className={styles.sectionTitle}><span>{t('topics')}</span><small>{tags.length}</small></div>
          <nav className={styles.topics} aria-label={t('topics')}>
            <button aria-current={!activeTag ? 'true' : undefined} onClick={() => setActiveTag('')}><span className={styles.topicIcon}><Layers3 /></span><span><strong>{t('allTopics')}</strong><small>{entries.length} {t('nodes')}</small></span></button>
            {tags.map((tag, index) => <button key={tag.name} aria-current={activeTag === tag.name ? 'true' : undefined} onClick={() => setActiveTag(tag.name)}>
              <span className={`${styles.topicDot} ${styles[`color${index % PALETTE_SIZE}`]}`} />
              <span><strong>{tag.name}</strong><small>{tag.terms} {t('termsShort')} · {tag.thoughts} {t('thoughtsShort')}</small></span>
              <span className={styles.count}>{tag.total}</span>
            </button>)}
          </nav>
          <div className={styles.sectionTitle}><span>{activeTag || t('allIdeas')}</span><small>{listEntries.length}</small></div>
          <div className={styles.entryList}>
            {listEntries.map((entry) => <button key={entry.id} aria-current={entry.id === selected ? 'true' : undefined} onClick={() => pick(entry)}>
              <span className={entry.kind === 'term' ? styles.termIcon : styles.thoughtIcon}>{entry.kind === 'term' ? <BookOpen /> : <Lightbulb />}</span>
              <span><strong>{entry.title}</strong><small>{entry.kind === 'term' ? t('term') : t('thought')}</small></span>
            </button>)}
            {!listEntries.length && <p className={styles.noResults}>{t('noResults')}</p>}
          </div>
        </aside>

        <div className={styles.graphPanel}>
          <div className={styles.graphToolbar}>
            <div className={styles.mode} role="group" aria-label={t('graphDepth')}>
              <button aria-pressed={mode === 'terms'} onClick={() => setGraphMode('terms')}><CircleDot />{t('termsOnly')}</button>
              <button aria-pressed={mode === 'expanded'} onClick={() => setGraphMode('expanded')}><GitFork />{t('termsAndThoughts')}</button>
            </div>
            <div className={styles.graphStats}><span><b>{shownEntries.filter((entry) => entry.kind === 'term').length}</b> {t('termsShort')}</span>{mode === 'expanded' && <span><b>{shownEntries.filter((entry) => entry.kind === 'thought').length}</b> {t('thoughtsShort')}</span>}<span><b>{edges.length}</b> {t('linksShort')}</span></div>
          </div>
          <div className={styles.canvas} data-panning={Boolean(pan.current) || undefined}>
            <svg role="img" aria-label={t('graphCanvas')} viewBox={`0 0 ${WIDTH} ${HEIGHT}`} onWheel={onWheel} onPointerDown={onPointerDown} onPointerMove={onPointerMove} onPointerUp={stopPan} onPointerCancel={stopPan}>
              <defs><pattern id="graph-grid" width="28" height="28" patternUnits="userSpaceOnUse"><circle cx="1" cy="1" r="1" /></pattern></defs>
              <rect className={styles.grid} width={WIDTH} height={HEIGHT} />
              <g transform={`translate(${transform.x} ${transform.y}) scale(${transform.scale})`} className={styles.viewport}>
                {graph.clusters.map((cluster) => <g key={cluster.name} className={`${styles.cluster} ${styles[`color${cluster.color}`]}`}>
                  <ellipse cx={cluster.x} cy={cluster.y} rx={cluster.rx} ry={cluster.ry} />
                  <text x={cluster.x - cluster.rx + 18} y={cluster.y - cluster.ry + 26}>{cluster.name === UNTAGGED ? t('untagged') : cluster.name}</text>
                </g>)}
                <g className={styles.edges}>{edges.map((edge) => {
                  const source = nodesByID.get(edge.source), target = nodesByID.get(edge.target);
                  return source && target ? <path key={edge.id} d={curve(source, target)} data-active={selected === source.id || selected === target.id || undefined} /> : null;
                })}</g>
                <g className={styles.nodes}>{graph.nodes.map((node) => {
                  const dimmed = Boolean(normalizedSearch && !matches.has(node.id));
                  return <g key={node.id} data-node data-kind={node.kind} data-selected={selected === node.id || undefined} data-dimmed={dimmed || undefined} className={`${styles.node} ${styles[`color${node.color}`]}`} transform={`translate(${node.x} ${node.y})`} style={{ '--delay': `${Math.min(node.order * 22, 280)}ms` } as CSSProperties} role="button" tabIndex={0} aria-label={`${node.kind === 'term' ? t('term') : t('thought')}: ${node.title}`} onClick={() => pick(node)} onKeyDown={(event) => nodeKey(event, node)}>
                    <circle className={styles.nodeHalo} r={node.kind === 'term' ? 34 : 28} />
                    {node.kind === 'term' ? <><circle className={styles.nodeShape} r="21" /><circle className={styles.nodeCore} r="5" /></> : <><rect className={styles.nodeShape} x="-14" y="-14" width="28" height="28" rx="8" transform="rotate(45)" /><circle className={styles.nodeCore} r="4" /></>}
                    <text y={node.kind === 'term' ? 42 : 37} textAnchor="middle">{shortTitle(node.title)}</text>
                  </g>;
                })}</g>
              </g>
            </svg>
            <div className={styles.zoom} role="group" aria-label={t('zoom')}><button aria-label={t('zoomIn')} onClick={() => zoom(.15)}><Plus /></button><button aria-label={t('zoomOut')} onClick={() => zoom(-.15)}><Minus /></button><button aria-label={t('fitGraph')} onClick={() => setTransform({ x: 0, y: 0, scale: 1 })}><Focus /></button></div>
            <div className={styles.legend}><span><i className={styles.termLegend} />{t('terms')}</span>{mode === 'expanded' && <span><i className={styles.thoughtLegend} />{t('thoughts')}</span>}<span className={styles.dragHint}>{t('dragHint')}</span></div>
            {current && <article className={styles.detail}>
              <button className={styles.detailClose} aria-label={t('close')} onClick={() => setSelected('')}>×</button>
              <span className={styles.kind}>{current.kind === 'term' ? <BookOpen /> : <Lightbulb />}{current.kind === 'term' ? t('term') : t('thought')}</span>
              <h2>{current.title}</h2><p>{current.text}</p>
              <div className={styles.tags}>{current.tags.map((tag) => <button key={tag} onClick={() => setActiveTag(tag)}><Tag />{tag}</button>)}</div>
              {Boolean(related.length) && <section className={styles.related}><h3>{t('connections')}</h3>{related.map((entry) => <button key={entry.id} onClick={() => pick(entry)}>{entry.kind === 'term' ? <BookOpen /> : <Lightbulb />}<span>{entry.title}</span><ChevronRight /></button>)}</section>}
            </article>}
          </div>
        </div>
      </section>}
  </main>;
}
