import { useMemo, useRef, useState, type CSSProperties, type KeyboardEvent, type PointerEvent, type WheelEvent } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { BookOpen, ChevronRight, Focus, Layers3, Minus, Network, Plus, Search, Tag, TextQuote } from 'lucide-react';
import { api } from '../../lib/api';
import { openWindow } from '../../lib/bridge';
import { Failure, Loading } from '../../components/Feedback';
import styles from './Graph.module.css';

interface Term { id: string; name: string; description: string; tags: string[] }
interface Thought { id: string; thesis: string; body: string; tags: string[] }
interface Link { id?: string; term_id: string; thought_id: string }
interface Snapshot { revision: number; terms: Term[]; thoughts: Thought[]; links: Link[] }
type NodeKind = 'term' | 'thought';
interface Entry { id: string; title: string; text: string; tags: string[]; kind: NodeKind }
interface GraphEdge { id: string; source: string; target: string; thoughtIds: string[] }
interface GraphNode extends Entry { x: number; y: number; cluster: string; color: number; order: number; degree: number }
interface ClusterLabel { name: string; x: number; y: number; color: number }
interface MovingNode extends GraphNode { vx: number; vy: number }

const WIDTH = 1000;
const HEIGHT = 640;
const PALETTE_SIZE = 7;
const UNTAGGED = '__untagged__';
const goldenAngle = Math.PI * (3 - Math.sqrt(5));
const clamp = (value: number, minimum: number, maximum: number) => Math.min(maximum, Math.max(minimum, value));
const normalize = (value: string) => value.trim().toLocaleLowerCase();
const shortTitle = (value: string) => value.length > 25 ? `${value.slice(0, 24)}…` : value;

function hash(value: string) {
  let result = 2166136261;
  for (let index = 0; index < value.length; index += 1) result = Math.imul(result ^ value.charCodeAt(index), 16777619);
  return result >>> 0;
}

function buildEntries(snapshot: Snapshot): Entry[] {
  return [
    ...snapshot.terms.map((term) => ({ id: term.id, title: term.name, text: term.description, tags: term.tags || [], kind: 'term' as const })),
    ...snapshot.thoughts.map((thought) => ({ id: thought.id, title: thought.thesis, text: thought.body, tags: thought.tags || [], kind: 'thought' as const })),
  ];
}

function buildEdges(snapshot: Snapshot, visible: Set<string>): GraphEdge[] {
  const termsByThought = new Map<string, string[]>();
  for (const link of snapshot.links) {
    if (!visible.has(link.term_id)) continue;
    termsByThought.set(link.thought_id, [...(termsByThought.get(link.thought_id) || []), link.term_id]);
  }
  const edges = new Map<string, GraphEdge>();
  for (const [thoughtID, terms] of termsByThought) {
    const unique = [...new Set(terms)].sort();
    for (let left = 0; left < unique.length; left += 1) for (let right = left + 1; right < unique.length; right += 1) {
      const id = `${unique[left]}:${unique[right]}`;
      const edge = edges.get(id) || { id, source: unique[left], target: unique[right], thoughtIds: [] };
      edge.thoughtIds.push(thoughtID);
      edges.set(id, edge);
    }
  }
  return [...edges.values()];
}

function primaryTag(entry: Entry, tagOrder: string[]) {
  return [...entry.tags].sort((left, right) => {
    const leftOrder = tagOrder.indexOf(left), rightOrder = tagOrder.indexOf(right);
    if (leftOrder < 0) return 1;
    if (rightOrder < 0) return -1;
    return leftOrder - rightOrder;
  })[0] || UNTAGGED;
}

function segmentsCross(a: MovingNode, b: MovingNode, c: MovingNode, d: MovingNode) {
  const side = (p: MovingNode, q: MovingNode, r: MovingNode) => (q.x - p.x) * (r.y - p.y) - (q.y - p.y) * (r.x - p.x);
  return side(a, b, c) * side(a, b, d) < 0 && side(c, d, a) * side(c, d, b) < 0;
}

function layout(entries: Entry[], tagOrder: string[], edges: GraphEdge[]): { nodes: GraphNode[]; labels: ClusterLabel[] } {
  if (!entries.length) return { nodes: [], labels: [] };
  const degree = new Map<string, number>();
  for (const edge of edges) {
    degree.set(edge.source, (degree.get(edge.source) || 0) + 1);
    degree.set(edge.target, (degree.get(edge.target) || 0) + 1);
  }
  const grouped = new Map<string, Entry[]>();
  for (const entry of entries) {
    const cluster = primaryTag(entry, tagOrder);
    grouped.set(cluster, [...(grouped.get(cluster) || []), entry]);
  }
  const names = [...grouped.keys()].sort((left, right) => {
    const leftOrder = tagOrder.indexOf(left), rightOrder = tagOrder.indexOf(right);
    if (leftOrder >= 0 && rightOrder >= 0 && leftOrder !== rightOrder) return leftOrder - rightOrder;
    return left.localeCompare(right);
  });
  const orbit = names.length <= 1 ? 0 : Math.min(228, 135 + names.length * 18);
  const centers = new Map(names.map((name, index) => {
    if (names.length === 1) return [name, { x: WIDTH / 2, y: HEIGHT / 2, color: 0 }] as const;
    const noise = ((hash(name) % 1000) / 1000 - .5);
    const angle = -Math.PI / 2 + index * Math.PI * 2 / names.length + noise * .42;
    const radius = orbit + noise * 38;
    return [name, { x: WIDTH / 2 + Math.cos(angle) * radius, y: HEIGHT / 2 + Math.sin(angle) * radius * .73, color: index % PALETTE_SIZE }] as const;
  }));
  const nodes: MovingNode[] = [];
  for (const [name, members] of grouped) {
    const center = centers.get(name)!;
    members.sort((left, right) => (degree.get(right.id) || 0) - (degree.get(left.id) || 0) || left.title.localeCompare(right.title));
    members.forEach((entry, index) => {
      const seed = (hash(entry.id) % 1000) / 1000;
      const radius = index === 0 ? 0 : 31 * Math.sqrt(index) + seed * 13;
      const angle = index * goldenAngle + seed * Math.PI * 2;
      nodes.push({ ...entry, cluster: name, color: center.color, order: nodes.length, degree: degree.get(entry.id) || 0, x: center.x + Math.cos(angle) * radius, y: center.y + Math.sin(angle) * radius, vx: 0, vy: 0 });
    });
  }
  const byID = new Map(nodes.map((node) => [node.id, node]));
  const iterations = nodes.length > 300 ? 45 : nodes.length > 120 ? 65 : 95;
  for (let iteration = 0; iteration < iterations; iteration += 1) {
    for (const node of nodes) {
      const center = centers.get(node.cluster)!;
      node.vx += (center.x - node.x) * .018 + (WIDTH / 2 - node.x) * .0007;
      node.vy += (center.y - node.y) * .018 + (HEIGHT / 2 - node.y) * .0007;
    }
    for (const edge of edges) {
      const source = byID.get(edge.source), target = byID.get(edge.target);
      if (!source || !target) continue;
      const dx = target.x - source.x, dy = target.y - source.y;
      const distance = Math.max(1, Math.hypot(dx, dy));
      const pull = (distance - 86) * .0065;
      source.vx += dx / distance * pull; source.vy += dy / distance * pull;
      target.vx -= dx / distance * pull; target.vy -= dy / distance * pull;
    }
    const cellSize = 38;
    const cells = new Map<string, MovingNode[]>();
    for (const node of nodes) {
      const cellX = Math.floor(node.x / cellSize), cellY = Math.floor(node.y / cellSize);
      for (let x = cellX - 1; x <= cellX + 1; x += 1) for (let y = cellY - 1; y <= cellY + 1; y += 1) {
        for (const other of cells.get(`${x}:${y}`) || []) {
          let dx = node.x - other.x, dy = node.y - other.y;
          if (!dx && !dy) { dx = .01; dy = -.01; }
          const distance = Math.max(1, Math.hypot(dx, dy));
          const minimum = node.cluster === other.cluster ? 52 : 64;
          if (distance >= minimum) continue;
          const push = (minimum - distance) * .06;
          node.vx += dx / distance * push; node.vy += dy / distance * push;
          other.vx -= dx / distance * push; other.vy -= dy / distance * push;
        }
      }
      const key = `${cellX}:${cellY}`;
      cells.set(key, [...(cells.get(key) || []), node]);
    }
    if (edges.length <= 120 && iteration % 12 === 0) {
      for (let left = 0; left < edges.length; left += 1) for (let right = left + 1; right < edges.length; right += 1) {
        const first = edges[left], second = edges[right];
        if ([first.source, first.target].some((id) => id === second.source || id === second.target)) continue;
        const a = byID.get(first.source), b = byID.get(first.target), c = byID.get(second.source), d = byID.get(second.target);
        if (!a || !b || !c || !d || !segmentsCross(a, b, c, d)) continue;
        const length = Math.max(1, Math.hypot(b.x - a.x, b.y - a.y));
        const direction = hash(`${first.id}:${second.id}`) % 2 ? 1 : -1;
        const nx = -(b.y - a.y) / length * direction * .8, ny = (b.x - a.x) / length * direction * .8;
        a.vx += nx; a.vy += ny; b.vx += nx; b.vy += ny; c.vx -= nx; c.vy -= ny; d.vx -= nx; d.vy -= ny;
      }
    }
    for (const node of nodes) {
      node.vx = clamp(node.vx * .78, -8, 8); node.vy = clamp(node.vy * .78, -8, 8);
      node.x = clamp(node.x + node.vx, 65, WIDTH - 65); node.y = clamp(node.y + node.vy, 62, HEIGHT - 62);
    }
  }
  const labels = names.map((name) => {
    const members = nodes.filter((node) => node.cluster === name);
    return { name, color: centers.get(name)!.color, x: members.reduce((sum, node) => sum + node.x, 0) / members.length, y: clamp(Math.min(...members.map((node) => node.y)) - 38, 28, HEIGHT - 30) };
  });
  return { nodes: nodes.map(({ vx: _vx, vy: _vy, ...node }) => node), labels };
}

function curve(source: GraphNode, target: GraphNode, edge: GraphEdge) {
  const dx = target.x - source.x, dy = target.y - source.y;
  const length = Math.max(1, Math.hypot(dx, dy));
  const bend = (hash(edge.id) % 2 ? 1 : -1) * Math.min(18, length * .055);
  const middleX = (source.x + target.x) / 2 - dy / length * bend;
  const middleY = (source.y + target.y) / 2 + dx / length * bend;
  return `M ${source.x} ${source.y} Q ${middleX} ${middleY} ${target.x} ${target.y}`;
}

export function Graph() {
  const { t } = useTranslation();
  const [search, setSearch] = useState('');
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
  const shownTerms = useMemo(() => entries.filter((entry) => entry.kind === 'term' && (!activeTag || entry.tags.includes(activeTag))), [entries, activeTag]);
  const visibleIDs = useMemo(() => new Set(shownTerms.map((entry) => entry.id)), [shownTerms]);
  const edges = useMemo(() => query.data ? buildEdges(query.data, visibleIDs) : [], [query.data, visibleIDs]);
  const graph = useMemo(() => layout(shownTerms, tags.map((tag) => tag.name), edges), [shownTerms, tags, edges]);
  const nodesByID = useMemo(() => new Map(graph.nodes.map((node) => [node.id, node])), [graph.nodes]);
  const labelsByCluster = useMemo(() => new Map(graph.labels.map((label) => [label.name, label])), [graph.labels]);
  const normalizedSearch = normalize(search);
  const matches = useMemo(() => new Set(entries.filter((entry) => !normalizedSearch || normalize(`${entry.title} ${entry.text} ${entry.tags.join(' ')}`).includes(normalizedSearch)).map((entry) => entry.id)), [entries, normalizedSearch]);
  const listEntries = entries.filter((entry) => (!activeTag || entry.tags.includes(activeTag)) && matches.has(entry.id));
  const current = entries.find((entry) => entry.id === selected);
  const related = current && query.data ? query.data.links
    .filter((link) => link.term_id === current.id || link.thought_id === current.id)
    .map((link) => entries.find((entry) => entry.id === (current.kind === 'term' ? link.thought_id : link.term_id)))
    .filter((entry): entry is Entry => Boolean(entry)) : [];
  const highlightedTerms = useMemo(() => new Set(current?.kind === 'thought' && query.data
    ? query.data.links.filter((link) => link.thought_id === current.id).map((link) => link.term_id)
    : current?.kind === 'term' ? [current.id] : []), [current, query.data]);

  if (!query.data) return query.isError ? <Failure retry={() => void query.refetch()} /> : <Loading />;

  const zoom = (delta: number) => setTransform((value) => ({ ...value, scale: clamp(value.scale + delta, .65, 2.2) }));
  const onWheel = (event: WheelEvent<SVGSVGElement>) => { event.preventDefault(); zoom(event.deltaY > 0 ? -.1 : .1); };
  const onPointerDown = (event: PointerEvent<SVGSVGElement>) => {
    if ((event.target as Element).closest('[data-node]')) return;
    event.currentTarget.setPointerCapture(event.pointerId);
    pan.current = { x: event.clientX, y: event.clientY, originX: transform.x, originY: transform.y };
  };
  const onPointerMove = (event: PointerEvent<SVGSVGElement>) => {
    if (!pan.current) return;
    setTransform((value) => ({ ...value, x: pan.current!.originX + event.clientX - pan.current!.x, y: pan.current!.originY + event.clientY - pan.current!.y }));
  };
  const pick = (entry: Entry) => setSelected(entry.id);
  const nodeKey = (event: KeyboardEvent<SVGGElement>, node: GraphNode) => {
    if (event.key === 'Enter' || event.key === ' ') { event.preventDefault(); pick(node); }
  };

  return <main className={styles.page}>
    <header className={styles.header}><h1>{t('memoryTitle')}</h1></header>
    {!entries.length ? <div className={styles.empty}><Network /><h2>{t('graphEmpty')}</h2><p>{t('graphEmptyHint')}</p><button className="primary" onClick={() => void openWindow('review')}>{t('review')}<ChevronRight /></button></div> :
      <section className={styles.workspace}>
        <aside className={styles.sidebar}>
          <label className={styles.search}><Search /><input type="search" placeholder={t('graphSearch')} aria-label={t('graphSearch')} value={search} onChange={(event) => setSearch(event.target.value)} /></label>
          <div className={styles.sectionTitle}><span>{t('topics')}</span><small>{tags.length}</small></div>
          <nav className={styles.topics} aria-label={t('topics')}>
            <button aria-current={!activeTag ? 'true' : undefined} onClick={() => setActiveTag('')}><span className={styles.topicIcon}><Layers3 /></span><span><strong>{t('allTopics')}</strong><small>{entries.length} {t('nodes')}</small></span></button>
            {tags.map((tag, index) => <button key={tag.name} aria-current={activeTag === tag.name ? 'true' : undefined} onClick={() => setActiveTag(tag.name)}>
              <span className={`${styles.topicDot} ${styles[`color${index % PALETTE_SIZE}`]}`} /><span><strong>{tag.name}</strong><small>{tag.terms} {t('termsShort')} · {tag.thoughts} {t('thoughtsShort')}</small></span><span className={styles.count}>{tag.total}</span>
            </button>)}
          </nav>
          <div className={styles.entryList}>
            {listEntries.map((entry) => <button key={entry.id} aria-current={entry.id === selected ? 'true' : undefined} onClick={() => pick(entry)}>
              <span className={entry.kind === 'term' ? styles.termIcon : styles.thoughtIcon}>{entry.kind === 'term' ? <BookOpen /> : <TextQuote />}</span>
              <span><strong>{entry.title}</strong><small>{entry.kind === 'term' ? t('term') : t('thought')}</small></span>
            </button>)}
            {!listEntries.length && <p className={styles.noResults}>{t('noResults')}</p>}
          </div>
        </aside>
        <div className={styles.canvas} data-panning={Boolean(pan.current) || undefined}>
          <svg role="img" aria-label={t('graphCanvas')} viewBox={`0 0 ${WIDTH} ${HEIGHT}`} onWheel={onWheel} onPointerDown={onPointerDown} onPointerMove={onPointerMove} onPointerUp={() => { pan.current = null; }} onPointerCancel={() => { pan.current = null; }}>
            <g transform={`translate(${transform.x} ${transform.y}) scale(${transform.scale})`}>
              <g className={styles.clusterLabels}>{graph.labels.map((label) => <text key={label.name} x={label.x} y={label.y} textAnchor="middle" className={styles[`color${label.color}`]}>{label.name === UNTAGGED ? t('untagged') : label.name}</text>)}</g>
              <g className={styles.edges}>{edges.map((edge) => {
                const source = nodesByID.get(edge.source), target = nodesByID.get(edge.target);
                const active = current?.kind === 'thought' ? edge.thoughtIds.includes(current.id) : highlightedTerms.has(edge.source) || highlightedTerms.has(edge.target);
                return source && target ? <path key={edge.id} d={curve(source, target, edge)} data-active={active || undefined} /> : null;
              })}</g>
              <g>{graph.nodes.map((node) => {
                const dimmed = Boolean(normalizedSearch && !matches.has(node.id));
                const relatedHighlight = current?.kind === 'thought' && highlightedTerms.has(node.id);
                const cluster = labelsByCluster.get(node.cluster);
                const dx = node.x - (cluster?.x || WIDTH / 2), dy = node.y - (cluster?.y || HEIGHT / 2);
                const horizontal = Math.abs(dx) > Math.abs(dy) * 1.2;
                const labelX = horizontal ? (dx >= 0 ? 15 : -15) : 0;
                const labelY = horizontal ? 3 : dy >= 0 ? 24 : -13;
                const anchor = horizontal ? (dx >= 0 ? 'start' : 'end') : 'middle';
                return <g key={node.id} data-node data-selected={selected === node.id || relatedHighlight || undefined} data-dimmed={dimmed || undefined} className={`${styles.node} ${styles[`color${node.color}`]}`} transform={`translate(${node.x} ${node.y})`} style={{ '--delay': `${Math.min(node.order * 16, 180)}ms` } as CSSProperties} role="button" tabIndex={0} aria-label={`${t('term')}: ${node.title}`} onClick={() => pick(node)} onKeyDown={(event) => nodeKey(event, node)}>
                  <circle className={styles.nodeHalo} r="17" /><circle className={styles.nodeShape} r={node.degree > 2 ? 9 : 7} /><text x={labelX} y={labelY} textAnchor={anchor}>{shortTitle(node.title)}</text>
                </g>;
              })}</g>
            </g>
          </svg>
          <div className={styles.zoom} role="group" aria-label={t('zoom')}><button aria-label={t('zoomIn')} onClick={() => zoom(.15)}><Plus /></button><button aria-label={t('zoomOut')} onClick={() => zoom(-.15)}><Minus /></button><button aria-label={t('fitGraph')} onClick={() => setTransform({ x: 0, y: 0, scale: 1 })}><Focus /></button></div>
          {current && <article className={styles.detail}>
            <button className={styles.detailClose} aria-label={t('close')} onClick={() => setSelected('')}>×</button>
            <span className={styles.kind}>{current.kind === 'term' ? <BookOpen /> : <TextQuote />}{current.kind === 'term' ? t('term') : t('thought')}</span>
            <h2>{current.title}</h2><p>{current.text}</p>
            <div className={styles.tags}>{current.tags.map((tag) => <button key={tag} onClick={() => setActiveTag(tag)}><Tag />{tag}</button>)}</div>
            {Boolean(related.length) && <section className={styles.related}><h3>{t('connections')}</h3>{related.map((entry) => <button key={entry.id} onClick={() => pick(entry)}>{entry.kind === 'term' ? <BookOpen /> : <TextQuote />}<span>{entry.title}</span><ChevronRight /></button>)}</section>}
          </article>}
        </div>
      </section>}
  </main>;
}
