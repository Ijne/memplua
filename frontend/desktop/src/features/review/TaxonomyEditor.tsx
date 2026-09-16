import { useEffect } from 'react';
import { ArrowRightLeft, CircleSlash, Plus, Tag as TagIcon } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useDraftAutosave } from './autosave';
import type { EditorHandle } from './ItemEditor';
import { SaveIndicator } from './SaveIndicator';
import { TagSelector } from './TagSelector';
import type { Tag, TaxonomyDecision, TaxonomyItem } from './types';
import styles from './Review.module.css';

export function TaxonomyEditor({ item, tags, save, register }: { item: TaxonomyItem; tags: Tag[]; save: (decision: TaxonomyDecision) => Promise<void>; register: (handle: EditorHandle | null) => void }) {
  const { t } = useTranslation('review');
  const saver = useDraftAutosave({ resolution: item.resolution, value: item.value, target_id: item.target_id || '' }, async draft => {
    if (!draft.resolution || (draft.resolution === 'create' && !draft.value.trim()) || (draft.resolution === 'map' && !draft.target_id)) throw new Error('invalidTag');
    await save({ resolution: draft.resolution, ...(draft.resolution === 'create' ? { value: draft.value } : {}), ...(draft.resolution === 'map' ? { target_id: draft.target_id } : {}) });
  });
  useEffect(() => { register({ flush: () => saver.flush() }); return () => register(null); }, [register, saver]);
  const draft = saver.value;
  const target = tags.find(tag => tag.id === draft.target_id);
  return <div className={styles.taxonomyPage}>
    <div className={styles.taxonomyHeading}><span className={styles.largeIcon}><TagIcon size={25} /></span><div><h2>{t('tagReview')}</h2><p>{t('tagReviewHint')}</p></div></div>
    <section className={styles.taxonomyCard}>
      <div className={styles.taxonomyTop}><span className={styles.chip}>{item.value}<span className={styles.newLabel}>{t('newTag')}</span></span><SaveIndicator status={saver.status === 'idle' && item.resolution ? 'saved' : saver.status} retry={() => { void saver.flush(); }} /></div>
      <div className={styles.tagSegments} role="group" aria-label={t('tagReview')}>
        {([{ value: 'create', label: 'createTag', icon: Plus }, { value: 'map', label: 'mapTag', icon: ArrowRightLeft }, { value: 'remove', label: 'rejectTag', icon: CircleSlash }] as const).map(({ value, label, icon: Icon }) => <button type="button" key={value} aria-pressed={draft.resolution === value} onClick={() => {
          saver.edit({ ...draft, resolution: value });
        }}><Icon size={16} />{t(label)}</button>)}
      </div>
      {draft.resolution === 'create' && <><label className={styles.field}><span>{t('tagName')}</span><input value={draft.value} onChange={event => saver.edit({ ...draft, value: event.target.value })} /></label><p className={styles.hint}>{t('tagCreateHint')}</p></>}
      {draft.resolution === 'map' && <div className={styles.field}><span>{t('mapTarget')}</span><TagSelector multiple={false} label={t('selectTag')} tags={tags} value={target ? [target.name] : []} onChange={names => saver.edit({ ...draft, target_id: tags.find(tag => tag.name === names[0])?.id || '' })} /></div>}
      {draft.resolution === 'remove' && <div className={styles.decisionNotice}><CircleSlash size={22} /><p>{t('rejectHint')}</p></div>}
      {!draft.resolution && <p className={styles.hint}>{t('chooseDecision')}</p>}
      {item.required && <p className={styles.hint}>{t('tagRequired')}</p>}
      {saver.status === 'failed' && <p role="alert" className={styles.inlineError}>{saver.error instanceof Error && saver.error.message === 'invalidTag' ? t('invalidTag') : t('saveFailed')}</p>}
    </section>
  </div>;
}
