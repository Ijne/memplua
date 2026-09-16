import { useEffect, useState } from 'react';
import { BookOpen, Check, CircleSlash, Layers3, Lightbulb, Plus, RefreshCw, Sparkles, Tag as TagIcon } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useDraftAutosave } from './autosave';
import { ApiError } from '../../lib/api';
import { SaveIndicator } from './SaveIndicator';
import { TagSelector } from './TagSelector';
import { bodyOf, titleOf, type Decision, type Item, type Resolution, type Tag, type TaxonomyDecision, type TaxonomyItem, type Value } from './types';
import styles from './Review.module.css';

export type EditorHandle = { flush: () => Promise<boolean> };
type Draft = { resolution: Resolution | ''; targetId: string; value: Value; relatedItemIds: string[]; tagEdits: Record<string, string> };
type Props = {
  item: Item;
  allItems: Item[];
  tags: Tag[];
  taxonomy: TaxonomyItem[];
  save: (decision: Decision) => Promise<void>;
  saveTaxonomy: (id: string, decision: TaxonomyDecision) => Promise<void>;
  register: (handle: EditorHandle | null) => void;
};

const createDecisions = [{ value: 'create', icon: Plus }, { value: 'reject', icon: CircleSlash }] as const;
const existingDecisions = [{ value: 'keep_existing', icon: Check }, { value: 'update', icon: RefreshCw }] as const;
const normalize = (value: string) => value.trim().toLocaleLowerCase();
const itemTitle = (item: Item) => titleOf(item.final_value) || titleOf(item.incoming_variants[0]?.value || { tags: [] });

function initialRelationships(item: Item, allItems: Item[]) {
  if (item.related_item_ids) return item.related_item_ids;
  const names = new Set(item.incoming_variants.flatMap(variant => variant.related_terms || []).map(normalize));
  return allItems.filter(candidate => candidate.kind === 'term' && names.has(normalize(itemTitle(candidate)))).map(candidate => candidate.id);
}

function connectedItems(item: Item, allItems: Item[], relatedItemIds: string[]) {
  if (item.kind === 'thought') return allItems.filter(candidate => relatedItemIds.includes(candidate.id));
  const thoughts = allItems.filter(candidate => candidate.kind === 'thought' && (candidate.related_item_ids || []).includes(item.id));
  const connected = new Set(thoughts.map(candidate => candidate.id));
  for (const thought of thoughts) {
    for (const termID of thought.related_item_ids || []) if (termID !== item.id) connected.add(termID);
  }
  return allItems.filter(candidate => connected.has(candidate.id));
}

export function ItemEditor({ item, allItems, tags, taxonomy, save, saveTaxonomy, register }: Props) {
  const { t } = useTranslation('review');
  const incoming = item.incoming_variants || [];
  const canonicalMatches = (item.canonical_matches || []).filter(match => match.source === 'canonical');
  const exact = canonicalMatches.find(match => match.exact);
  // An exact title collision is mandatory and is the only meaningful target.
  // Hiding weaker suggestions prevents the user from accidentally focusing a
  // different entity while resolving that conflict.
  const matches = exact ? [exact] : canonicalMatches;
  const initialValue = titleOf(item.final_value) ? item.final_value : incoming[0]?.value || { tags: [] };
  const initialTagEdits = Object.fromEntries(taxonomy
    .filter(entry => (initialValue.tags || []).some(name => normalize(name) === normalize(entry.value)))
    .map(entry => [entry.id, entry.value]));
  const saver = useDraftAutosave<Draft>({
    resolution: exact && !['update', 'keep_existing'].includes(item.resolution) ? '' : item.resolution,
    targetId: exact?.id || item.target_id || '',
    value: { ...initialValue, tags: [...(initialValue.tags || [])] },
    relatedItemIds: initialRelationships(item, allItems),
    tagEdits: initialTagEdits,
  }, async draft => {
    if (!draft.resolution) return;
    const rejectedRelated = allItems.filter(candidate => draft.relatedItemIds.includes(candidate.id) && candidate.resolution === 'reject');
    if (item.kind === 'thought' && draft.resolution !== 'reject' && rejectedRelated.length) throw new Error('rejectedDependency');
    const decision: Decision = { resolution: draft.resolution };
    if (draft.resolution === 'update' || draft.resolution === 'keep_existing') {
      const target = matches.find(match => match.id === draft.targetId);
      if (!target) throw new Error('selectTarget');
      decision.target_id = target.id;
      decision.target_version = target.version;
    }
    if (draft.resolution === 'create' || draft.resolution === 'update') {
      if (!titleOf(draft.value).trim() || (item.kind === 'term' && !draft.value.tags?.length)) throw new Error('invalidValue');
      for (const [id, editedName] of Object.entries(draft.tagEdits)) {
        if (!(draft.value.tags || []).some(name => normalize(name) === normalize(editedName))) continue;
        if (!editedName.trim()) throw new Error('invalidTag');
        const existing = tags.find(tag => [tag.name, ...(tag.aliases || [])].some(name => normalize(name) === normalize(editedName)));
        await saveTaxonomy(id, existing ? { resolution: 'map', target_id: existing.id } : { resolution: 'create', value: editedName });
      }
      decision.final_value = draft.value;
    }
    if (item.kind === 'thought') decision.related_item_ids = draft.relatedItemIds;
    await save(decision);
  });
  useEffect(() => { register({ flush: () => saver.flush() }); return () => register(null); }, [register, saver]);
  const draft = saver.value;
  const selected = matches.find(match => match.id === draft.targetId);
  const [previewResolution, setPreviewResolution] = useState<Resolution | ''>('');
  const editable = draft.resolution !== 'keep_existing' && draft.resolution !== 'reject';
  const shownValue = draft.resolution === 'keep_existing' && selected ? selected.value : draft.value;
  const related = connectedItems(item, allItems, draft.relatedItemIds);
  const terms = allItems.filter(candidate => candidate.kind === 'term');
  const rejectedRelated = terms.filter(candidate => draft.relatedItemIds.includes(candidate.id) && candidate.resolution === 'reject');
  const focused = Boolean(selected);
  const availableDecisions = focused ? existingDecisions : createDecisions;
  const fieldDisposition = draft.resolution || previewResolution;
  const KindIcon = item.kind === 'term' ? BookOpen : Lightbulb;

  function chooseResolution(resolution: Resolution) {
    const target = selected || exact;
    saver.edit({
      ...draft,
      resolution,
      targetId: resolution === 'update' || resolution === 'keep_existing' ? target?.id || '' : '',
    });
  }
  function editValue(value: Partial<Value>) { saver.edit({ ...draft, resolution: draft.resolution || (focused ? 'update' : 'create'), value: { ...draft.value, ...value } }); }
  function chooseTarget(id: string) {
    if (!exact && draft.targetId === id) {
      saver.edit({ ...draft, targetId: '', resolution: draft.resolution === 'update' || draft.resolution === 'keep_existing' ? '' : draft.resolution });
      return;
    }
    saver.edit({ ...draft, targetId: id, resolution: '' });
  }
  function renameNewTag(id: string, originalName: string, value: string) {
    const tagsValue = [...(draft.value.tags || [])];
    const currentName = draft.tagEdits[id] ?? originalName;
    const index = tagsValue.findIndex(name => normalize(name) === normalize(currentName) || normalize(name) === normalize(originalName));
    if (index < 0) return;
    tagsValue[index] = value;
    saver.edit({
      ...draft,
      resolution: draft.resolution || (focused ? 'update' : 'create'),
      value: { ...draft.value, tags: tagsValue },
      tagEdits: { ...draft.tagEdits, [id]: value },
    });
  }
  function toggleRelationship(id: string) {
    saver.edit({
      ...draft,
      resolution: draft.resolution || 'create',
      relatedItemIds: draft.relatedItemIds.includes(id) ? draft.relatedItemIds.filter(value => value !== id) : [...draft.relatedItemIds, id],
    });
  }

  return <>
    <div className={styles.entityStrip} data-kind={item.kind}>
      <span className={styles.kind} data-kind={item.kind}><KindIcon size={16} />{t(item.kind)}</span>
      <SaveIndicator status={saver.status === 'idle' && item.resolution ? 'saved' : saver.status} retry={() => { void saver.flush(); }} />
    </div>
    <div className={styles.columns}>
      <section className={`${styles.column} ${styles.proposed}`} data-kind={item.kind} aria-label={t('proposal')}>
        <h2 className={styles.columnLabel}><Sparkles size={16} />{t('proposal')}</h2>
        {incoming.map((variant, index) => <div key={variant.candidate_id} className={index ? styles.extraVariant : undefined}>
          <h3 className={styles.entityTitle}>{titleOf(variant.value)}</h3>
          <p className={styles.entityBody}>{bodyOf(variant.value)}</p>
          <div className={styles.chips}>{(variant.value.tags || []).map(name => {
            const isNew = !tags.some(tag => [tag.name, ...(tag.aliases || [])].some(value => normalize(value) === normalize(name)));
            const taxonomyItem = taxonomy.find(entry => normalize(entry.value) === normalize(name));
            const editedName = taxonomyItem ? draft.tagEdits[taxonomyItem.id] ?? name : name;
            return <span className={`${styles.chip} ${isNew ? styles.newTagChip : ''}`} data-new={isNew || undefined} key={taxonomyItem?.id || name}>
              <TagIcon size={11} />{isNew && taxonomyItem
                ? <input aria-label={t('editNewTag', { name })} value={editedName} size={Math.max(4, Math.min(24, editedName.length))} maxLength={80}
                  onChange={event => renameNewTag(taxonomyItem.id, name, event.target.value)}
                  onKeyDown={event => { if (event.key === 'Enter') { event.preventDefault(); event.currentTarget.blur(); } }} />
                : name}
              {isNew && <span className={styles.newLabel}>{t('newTag')}</span>}
            </span>;
          })}</div>
        </div>)}
      </section>
      <section className={styles.column} aria-label={t('canonical')}>
        <h2 className={styles.columnLabel}><Layers3 size={16} />{t('canonical')}</h2>
        <div className={styles.matches} role="group" aria-label={t('canonical')}>
          {matches.map(match => <button type="button" key={match.id} className={`${styles.match} ${draft.targetId === match.id ? styles.matchSelected : ''} ${match.exact ? styles.exactConflict : ''}`} aria-pressed={draft.targetId === match.id} aria-disabled={match.exact || undefined} onClick={() => chooseTarget(match.id)}>
            <span className={styles.matchBadge}>{draft.targetId === match.id && <Check size={12} />}{t(match.exact ? 'exactConflict' : 'similar')}</span>
            <strong>{titleOf(match.value)}</strong><span className={styles.matchBody}>{bodyOf(match.value)}</span>
            <span className={styles.chips}>{(match.value.tags || []).map(tag => <span className={styles.chip} key={tag}>{tag}</span>)}</span>
          </button>)}
          {!matches.length && <div className={styles.noMatches}><Layers3 size={28} /><strong>{t('noMatches')}</strong><p>{t('noMatchesHint')}</p></div>}
        </div>
        <div className={styles.related}>
          <h4>{t('relatedInConspect')}</h4>
          <div className={styles.connectedList}>
            {related.map(candidate => {
              const RelatedIcon = candidate.kind === 'term' ? BookOpen : Lightbulb;
              return <div className={styles.connectedEntity} data-kind={candidate.kind} key={candidate.id}><RelatedIcon size={15} /><span><small>{t(candidate.kind)}</small><strong>{itemTitle(candidate)}</strong></span></div>;
            })}
            {!related.length && <p className={styles.hint}>{t('noRelatedEntities')}</p>}
          </div>
        </div>
      </section>
      <section className={`${styles.column} ${styles.result}`} aria-label={t('outcome')}>
        <h2 className={styles.columnLabel}><Check size={16} />{t('outcome')}</h2>
        <div className={styles.segments} role="group" aria-label={t('chooseDecision')}>
          {availableDecisions.map(({ value, icon: Icon }) => <button type="button" key={value} aria-pressed={draft.resolution === value} onMouseEnter={() => setPreviewResolution(value)} onMouseLeave={() => setPreviewResolution('')} onFocus={() => setPreviewResolution(value)} onBlur={() => setPreviewResolution('')} onClick={() => chooseResolution(value)}><Icon size={15} /><span>{t(value)}</span></button>)}
        </div>
        {!draft.resolution && <p className={styles.hint}>{t('chooseDecision')}</p>}
        {exact && !draft.resolution && <div className={`${styles.decisionNotice} ${styles.conflictNotice}`}><CircleSlash size={24} /><p>{t('exactConflictHint')}</p></div>}
        {draft.resolution === 'reject' ? <div className={styles.decisionNotice}><CircleSlash size={24} /><p>{t('rejectHint')}</p></div> : <div className={styles.editableFields} data-disposition={fieldDisposition}>
          {draft.resolution === 'keep_existing' && <p className={styles.notice}><Check size={15} />{t('keepHint')}</p>}
          <label className={styles.field}><span>{t(item.kind === 'term' ? 'name' : 'thesis')}</span><textarea rows={item.kind === 'term' ? 2 : 3} className={styles.titleInput} value={item.kind === 'term' ? shownValue.name || '' : shownValue.thesis || ''} readOnly={!editable} onChange={event => editValue(item.kind === 'term' ? { name: event.target.value } : { thesis: event.target.value })} /></label>
          <label className={styles.field}><span>{t(item.kind === 'term' ? 'description' : 'body')}</span><textarea rows={8} value={item.kind === 'term' ? shownValue.description || '' : shownValue.body || ''} readOnly={!editable} onChange={event => editValue(item.kind === 'term' ? { description: event.target.value } : { body: event.target.value })} /></label>
          <div className={styles.field}><span>{t('tags')}</span><TagSelector tags={tags} value={shownValue.tags || []} disabled={!editable} onChange={tagsValue => editValue({ tags: tagsValue })} /></div>
          {item.kind === 'thought' && <div className={styles.relationEditor}>
            <span>{t('entitiesInThought')}</span>
            <p>{t('entitiesInThoughtHint')}</p>
            <div className={styles.relationOptions} role="group" aria-label={t('entitiesInThought')}>
              {terms.map(term => <button type="button" data-kind="term" data-rejected={term.resolution === 'reject' || undefined} key={term.id} aria-pressed={draft.relatedItemIds.includes(term.id)} disabled={draft.resolution === 'reject'} onClick={() => toggleRelationship(term.id)}>
                {draft.relatedItemIds.includes(term.id) ? <Check size={14} /> : <Plus size={14} />}<span>{itemTitle(term)}</span>
              </button>)}
            </div>
            {!!rejectedRelated.length && <p role="alert" className={styles.dependencyError}>{t('rejectedDependency', { names: rejectedRelated.map(itemTitle).join(', ') })}</p>}
          </div>}
        </div>}
        {saver.status === 'failed' && <p role="alert" className={styles.inlineError}>{saver.error instanceof ApiError && saver.error.code === 'review.rejected_dependency' ? t('rejectedDependency', { names: rejectedRelated.map(itemTitle).join(', ') }) : saver.error instanceof ApiError && saver.error.code === 'review.exact_match' ? t('exactConflictHint') : saver.error instanceof Error && ['invalidValue', 'selectTarget', 'invalidTag', 'rejectedDependency'].includes(saver.error.message) ? t(saver.error.message) : t('saveFailed')}</p>}
      </section>
    </div>
  </>;
}
