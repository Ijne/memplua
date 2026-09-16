import { useCallback, useEffect, useRef, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { AlertCircle, ArrowLeft, ArrowRight, BookOpen, Check, ChevronLeft, ChevronRight, CircleCheck, FileText, Inbox, LoaderCircle, ShieldCheck, Tag as TagIcon } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { api, ApiError } from '../../lib/api';
import { openWindow } from '../../lib/bridge';
import { ItemEditor, type EditorHandle } from './ItemEditor';
import { detailKey, detailPath, summaryKey, titleOf, type Conspect, type Decision, type Resolution, type Summary, type Tag, type TaxonomyDecision } from './types';
import styles from './Review.module.css';

type Page = { kind: 'item'; id: string } | { kind: 'summary' };
const resolutions: Resolution[] = ['create', 'update', 'keep_existing', 'reject'];
const initialPage = (detail: Conspect): Page => {
  const item = detail.items.find(item => !item.resolution || item.status === 'pending' || item.status === 'failed') || detail.items[0];
  if (item) return { kind: 'item', id: item.id };
  return { kind: 'summary' };
};
const isReady = (detail: Conspect) => detail.status === 'ready_to_apply' && detail.items.every(item => !!item.resolution && item.status === 'resolved') && detail.taxonomy.every(tag => !tag.required || !!tag.resolution);

export function Review({ conspectID }: { conspectID?: string }) {
  const { t, i18n } = useTranslation('review');
  const queryClient = useQueryClient();
  const requested = conspectID || new URLSearchParams(window.location.search).get('conspect') || '';
  const [selectedId, setSelectedId] = useState(requested);
  const [page, setPage] = useState<Page | null>(null);
  const [busy, setBusy] = useState(false);
  const [conflict, setConflict] = useState(false);
  const [editorVersion, setEditorVersion] = useState(0);
  const editor = useRef<EditorHandle | null>(null);
  const switching = useRef(false);
  const orderedItems = useRef<{ conspect: string; ids: string[] }>({ conspect: '', ids: [] });
  const heading = useRef<HTMLHeadingElement>(null);
  const gesture = useRef<{ x: number; y: number } | null>(null);
  const wheel = useRef({ distance: 0, at: 0 });

  const summariesQuery = useQuery({ queryKey: summaryKey, queryFn: () => api<Summary[]>('/api/v1/review/conspects?view=desktop'), refetchInterval: 10000 });
  const summaries = [...(summariesQuery.data || [])].sort((a, b) => Date.parse(a.created_at) - Date.parse(b.created_at));
  useEffect(() => {
    if (!selectedId && summaries.length) { setSelectedId(summaries[0].id); setPage(null); }
  }, [selectedId, summaries]);
  const detailQuery = useQuery({ queryKey: detailKey(selectedId), queryFn: () => api<Conspect>(detailPath(selectedId)), enabled: !!selectedId, refetchInterval: 10000 });
  const tagsQuery = useQuery({ queryKey: ['desktop', 'tags'], queryFn: () => api<Tag[]>('/api/v1/taxonomy/tag') });
  const detail = detailQuery.data;
  const tags = tagsQuery.data || [];
  useEffect(() => {
    if (!detail) return;
    if (orderedItems.current.conspect !== detail.id) {
      orderedItems.current = { conspect: detail.id, ids: [...detail.items].sort((a, b) => Number(!!a.resolution) - Number(!!b.resolution)).map(item => item.id) };
      setPage(initialPage(detail));
    }
  }, [detail]);
  const register = useCallback((handle: EditorHandle | null) => { editor.current = handle; }, []);
  const refresh = useCallback(async () => {
    const next = await api<Conspect>(detailPath(selectedId));
    queryClient.setQueryData(detailKey(selectedId), next);
    void queryClient.invalidateQueries({ queryKey: summaryKey });
    return next;
  }, [queryClient, selectedId]);
  const saveItem = useMutation({ mutationFn: async ({ id, decision }: { id: string; decision: Decision }) => {
    try { await api<void>(`/api/v1/review/items/${encodeURIComponent(id)}`, { method: 'PUT', body: JSON.stringify(decision) }); }
    catch (error) { if (error instanceof ApiError && error.code === 'review.conflict') { await refresh(); setConflict(true); } throw error; }
    await refresh();
  } });
  const saveTag = useMutation({ mutationFn: async ({ id, decision }: { id: string; decision: TaxonomyDecision }) => {
    await api<void>(`/api/v1/review/taxonomy/${encodeURIComponent(id)}`, { method: 'PUT', body: JSON.stringify(decision) });
    await refresh();
  } });
  const apply = useMutation({ mutationFn: async () => {
    await api<void>(`/api/v1/review/conspects/${encodeURIComponent(selectedId)}/apply`, { method: 'POST' });
  }, onSuccess: async () => {
    queryClient.removeQueries({ queryKey: detailKey(selectedId) });
    queryClient.setQueryData<Summary[]>(summaryKey, old => (old || []).filter(summary => summary.id !== selectedId));
    orderedItems.current = { conspect: '', ids: [] };
    setSelectedId(''); setPage(null); setConflict(false);
    void queryClient.invalidateQueries({ queryKey: summaryKey });
    void queryClient.invalidateQueries({ queryKey: ['desktop', 'tags'] });
    void queryClient.invalidateQueries({ queryKey: ['ui-state'] });
  }, onError: async error => {
    if (error instanceof ApiError && error.status === 409) {
      const fresh = await refresh();
      const failed = fresh.items.find(item => item.status === 'pending' || item.status === 'failed' || !item.resolution);
      setPage(failed ? { kind: 'item', id: failed.id } : initialPage(fresh));
      setConflict(true); setEditorVersion(value => value + 1);
    }
  } });

  function steps(current: Conspect): { kind: 'item'; id: string }[] {
    const ids = orderedItems.current.conspect === current.id ? orderedItems.current.ids : current.items.map(item => item.id);
    return ids.filter(id => current.items.some(item => item.id === id)).map(id => ({ kind: 'item' as const, id }));
  }
  async function move(target: (current: Conspect) => Page | null, conspect?: string) {
    if (switching.current || apply.isPending) return;
    switching.current = true; setBusy(true);
    try {
      if (editor.current && !await editor.current.flush()) return;
      if (conspect) { setSelectedId(conspect); setPage(null); setConflict(false); apply.reset(); return; }
      const current = queryClient.getQueryData<Conspect>(detailKey(selectedId));
      if (!current) return;
      const next = target(current);
      if (next) { setPage(next); apply.reset(); requestAnimationFrame(() => heading.current?.focus()); }
    } finally { switching.current = false; setBusy(false); }
  }
  function navigate(direction: number) {
    void move(current => {
      const sequence = steps(current);
      if (page?.kind === 'summary') return direction < 0 ? sequence.at(-1) || null : null;
      const index = sequence.findIndex(step => step.kind === page?.kind && step.id === (page && 'id' in page ? page.id : ''));
      const next = sequence[index + direction];
      if (next) return next;
      return null;
    });
  }
  const navigateRef = useRef(navigate);
  navigateRef.current = navigate;
  useEffect(() => {
    const key = (event: KeyboardEvent) => {
      if (event.altKey && !event.ctrlKey && !event.metaKey && (event.key === 'ArrowLeft' || event.key === 'ArrowRight')) {
        event.preventDefault(); navigateRef.current(event.key === 'ArrowRight' ? 1 : -1);
      }
    };
    window.addEventListener('keydown', key);
    return () => window.removeEventListener('keydown', key);
  }, []);
  useEffect(() => { if (conspectID && conspectID !== selectedId) void move(() => null, conspectID); }, [conspectID]); // Bridge navigation also flushes the active draft.

  const loading = summariesQuery.isPending || (!!selectedId && detailQuery.isPending);
  const loadError = summariesQuery.isError || detailQuery.isError || tagsQuery.isError;
  const sequence = detail ? steps(detail) : [];
  const position = sequence.findIndex(step => page && 'id' in page && step.kind === page.kind && step.id === page.id);
  const canEdit = detail && ['review_pending', 'ready_to_apply'].includes(detail.status);
  const currentItem = canEdit && page?.kind === 'item' ? detail?.items.find(item => item.id === page.id) : undefined;
  const ready = detail ? isReady(detail) : false;
  const reviewed = detail?.items.filter(item => !!item.resolution && item.status === 'resolved').length || 0;
  const sourceKind = detail?.source_kind?.split('/').at(-1) || 'unknown';
  const sourceLabel = t(`source.${sourceKind}`, { defaultValue: t('source.unknown') });

  return <main className={styles.review}>
    <header className={styles.header}>
      <div className={styles.headerHeading}><span className={styles.eyebrow}><BookOpen size={15} />{t('review')}</span><h1 ref={heading} tabIndex={-1}>{detail?.topic || t(detail ? 'untitled' : 'inbox')}</h1>{detail && <p className={styles.metadata}>{new Intl.DateTimeFormat(i18n.language, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(detail.created_at))}<span>·</span>{sourceLabel}</p>}</div>
      <div className={styles.headerActions}>{!!summaries.length && <label className={styles.conspectPicker}><span>{t('conspects')}</span><select aria-label={t('selectConspect')} value={selectedId} disabled={busy || apply.isPending} onChange={event => { void move(() => null, event.target.value); }}>
        {summaries.map(summary => <option key={summary.id} value={summary.id}>{summary.topic || t('untitled')}</option>)}
      </select></label>}{detail && <button type="button" className={styles.secondaryButton} onClick={() => { void openWindow('source', selectedId); }}><FileText size={17} />{t('sourceText')}</button>}</div>
    </header>
    {detail && <div className={styles.progressRow}><span>{t('progress', { done: reviewed, total: detail.items.length })}</span><progress aria-label={t('progress', { done: reviewed, total: detail.items.length })} max={detail.items.length || 1} value={reviewed} /><div className={styles.smallNav}><button type="button" aria-label={t('previous')} title={`${t('previous')} (Alt+←)`} disabled={busy || apply.isPending || (position <= 0 && page?.kind !== 'summary')} onClick={() => navigate(-1)}><ChevronLeft size={18} /></button><button type="button" aria-label={t('next')} title={`${t('next')} (Alt+→)`} disabled={busy || apply.isPending || page?.kind === 'summary' || position === sequence.length - 1} onClick={() => navigate(1)}><ChevronRight size={18} /></button></div></div>}
    {conflict && <div className={styles.conflict} role="alert"><AlertCircle size={18} /><span>{t('conflict')}</span><button type="button" aria-label={t('rejectShort')} onClick={() => setConflict(false)}>×</button></div>}
    <div className={styles.workspace} onTouchStart={event => {
      if ((event.target as HTMLElement).closest('input,textarea,button,select,[role="listbox"]')) return;
      if (event.touches.length === 1) gesture.current = { x: event.touches[0].clientX, y: event.touches[0].clientY };
    }} onTouchEnd={event => {
      const start = gesture.current; gesture.current = null;
      if (!start || !event.changedTouches.length) return;
      const dx = event.changedTouches[0].clientX - start.x;
      const dy = event.changedTouches[0].clientY - start.y;
      if (Math.abs(dx) > 100 && Math.abs(dx) > Math.abs(dy) * 2) navigate(dx < 0 ? 1 : -1);
    }} onWheel={event => {
      if (Math.abs(event.deltaX) < Math.abs(event.deltaY) * 2 || (event.target as HTMLElement).closest('textarea,input,[role="listbox"]')) return;
      const now = performance.now();
      if (now - wheel.current.at < 700) return;
      wheel.current.distance += event.deltaX;
      if (Math.abs(wheel.current.distance) > 140) { navigate(wheel.current.distance > 0 ? 1 : -1); wheel.current = { distance: 0, at: now }; }
    }}>
      {loading ? <div className={styles.empty}><LoaderCircle className={styles.spin} size={28} /><p>{t('loading')}</p></div> : loadError ? <div className={styles.empty} role="alert"><AlertCircle size={30} /><p>{t('loadFailed')}</p><button type="button" className={styles.secondaryButton} onClick={() => { void summariesQuery.refetch(); void detailQuery.refetch(); void tagsQuery.refetch(); }}>{t('retry')}</button></div> : !detail ? <div className={styles.empty}><span className={styles.emptyIcon}><Inbox size={34} /></span><h2>{t('emptyTitle')}</h2><p>{t('emptyBody')}</p></div> : <>
        {currentItem && <ItemEditor key={`${detail.id}:${currentItem.id}:${editorVersion}`} item={currentItem} allItems={detail.items} tags={tags} taxonomy={detail.taxonomy} register={register}
          save={decision => saveItem.mutateAsync({ id: currentItem.id, decision })}
          saveTaxonomy={(id, decision) => saveTag.mutateAsync({ id, decision })} />}
        {!canEdit && <div className={styles.empty} role="status"><LoaderCircle className={styles.spin} /><p>{t(detail.status === 'failed' ? 'preparationFailed' : 'preparing')}</p>{detail.status === 'failed' && <button onClick={() => void openWindow('settings')}>{t('openSettings')}</button>}</div>}
        {canEdit && page?.kind === 'summary' && <div className={styles.summary}>
          <div className={styles.summaryHeading}><span className={styles.largeIcon}><CircleCheck size={27} /></span><h2>{t('summaryTitle')}</h2><p>{t('summaryHint')}</p></div>
          <div className={styles.summaryGroups}>{resolutions.map(resolution => <section className={styles.summaryGroup} key={resolution}><h3><span className={styles.operationDot} data-operation={resolution} />{t(`${resolution}Short`)}<span className={styles.count}>{detail.items.filter(item => item.resolution === resolution).length}</span></h3><ul>{detail.items.filter(item => item.resolution === resolution).map(item => <li key={item.id}><span>{titleOf(item.final_value) || titleOf(item.incoming_variants[0]?.value || { tags: [] })}</span><button type="button" className={styles.textButton} onClick={() => { void move(() => ({ kind: 'item', id: item.id })); }} aria-label={`${t('edit')}: ${titleOf(item.final_value)}`}>{t('edit')}</button></li>)}</ul>{!detail.items.some(item => item.resolution === resolution) && <p className={styles.hint}>{t('noOperations')}</p>}</section>)}</div>
          {!!detail.taxonomy.filter(tag => tag.required && tag.resolution === 'create').length && <section className={styles.createdTags}><h3><TagIcon size={16} />{t('createdTags')}</h3><div className={styles.chips}>{detail.taxonomy.filter(tag => tag.required && tag.resolution === 'create').map(tag => <span className={styles.chip} key={tag.id}>{tag.value}<span className={styles.newLabel}>{t('newTag')}</span></span>)}</div></section>}
          {apply.isError && !conflict && <p className={styles.inlineError} role="alert">{t('applyFailed')}</p>}
          {!ready && <p className={styles.hint}>{t('completeDecisions')}</p>}
          <button type="button" className={styles.applyButton} disabled={!ready || busy || apply.isPending} onClick={() => apply.mutate()}>{apply.isPending ? <LoaderCircle size={19} className={styles.spin} /> : <Check size={19} />}{t(apply.isPending ? 'applying' : 'apply')}</button>
        </div>}
      </>}
    </div>
    {detail && <footer className={styles.footer}><p><ShieldCheck size={15} />{t('draftOnly')}</p><div className={styles.footerNav}>
      <button type="button" className={styles.secondaryButton} disabled={busy || apply.isPending || (position <= 0 && page?.kind !== 'summary')} onClick={() => navigate(-1)}><ArrowLeft size={16} />{t(page?.kind === 'summary' ? 'backToReview' : 'previous')}</button>
      {page?.kind !== 'summary' && <><span className={styles.position}>{t('itemPosition', { index: position + 1, total: sequence.length })}</span>{ready
        ? <button type="button" className={`${styles.primaryButton} ${styles.finishButton}`} disabled={busy || apply.isPending} onClick={() => { void move(() => ({ kind: 'summary' })); }}><Check size={16} />{t('save')}</button>
        : <button type="button" className={styles.primaryButton} disabled={busy || apply.isPending || position === sequence.length - 1} onClick={() => navigate(1)}>{t('next')}<ArrowRight size={16} /></button>}</>}
    </div></footer>}
  </main>;
}
