import { useEffect, useId, useRef, useState } from 'react';
import { Check, ChevronDown, Search, X } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import type { Tag } from './types';
import styles from './Review.module.css';

type Props = {
  tags: Tag[];
  value: string[];
  onChange: (value: string[]) => void;
  onRenameNewTag?: (name: string, value: string) => void;
  disabled?: boolean;
  multiple?: boolean;
  label?: string;
};

export function TagSelector({ tags, value, onChange, onRenameNewTag, disabled, multiple = true, label }: Props) {
  const { t } = useTranslation('review');
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState('');
  const container = useRef<HTMLDivElement>(null);
  const input = useRef<HTMLInputElement>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  const listId = useId();
  const available = tags.filter(tag => tag.enabled);
  const normalized = search.trim().toLocaleLowerCase();
  const filtered = available.filter(tag => [tag.name, ...(tag.aliases || [])].some(name => name.toLocaleLowerCase().includes(normalized)));
  useEffect(() => {
    if (!open) return;
    input.current?.focus();
    const close = (event: PointerEvent) => { if (!container.current?.contains(event.target as Node)) setOpen(false); };
    const blur = () => setOpen(false);
    document.addEventListener('pointerdown', close);
    window.addEventListener('blur', blur);
    return () => { document.removeEventListener('pointerdown', close); window.removeEventListener('blur', blur); };
  }, [open]);

  function choose(name: string) {
    onChange(multiple ? value.includes(name) ? value.filter(value => value !== name) : [...value, name] : [name]);
    if (!multiple) { setOpen(false); trigger.current?.focus(); }
  }

  return <div ref={container} className={styles.tagSelector} onBlur={event => {
    if (!event.currentTarget.contains(event.relatedTarget)) setOpen(false);
  }} onKeyDown={event => {
    if (event.key === 'Escape' && open) { event.stopPropagation(); setOpen(false); trigger.current?.focus(); }
  }}>
    {!!value.length && <div className={styles.chips}>{value.map((name, index) => {
      const isNew = !available.some(tag => tag.name === name || tag.aliases?.includes(name));
      return <span className={`${styles.chip} ${isNew ? styles.newTagChip : ''}`} data-new={isNew || undefined} key={`${isNew ? 'new' : 'saved'}:${index}`}>
        {isNew && onRenameNewTag && !disabled
          ? <input aria-label={t('editNewTag', { name })} value={name} size={Math.max(4, Math.min(24, name.length))} maxLength={80}
            onChange={event => onRenameNewTag(name, event.target.value)}
            onKeyDown={event => { if (event.key === 'Enter') { event.preventDefault(); event.currentTarget.blur(); } }} />
          : name}
        {isNew && <span className={styles.newLabel}>{t('newTag')}</span>}
        {!disabled && <button type="button" aria-label={t('removeTag', { name })} onClick={() => onChange(value.filter((_, valueIndex) => valueIndex !== index))}><X size={12} /></button>}
      </span>;
    })}</div>}
    <button type="button" className={styles.tagTrigger} ref={trigger} disabled={disabled} aria-haspopup="listbox" aria-expanded={open} aria-controls={open ? listId : undefined} onClick={() => { setSearch(''); setOpen(!open); }}>
      <span>{label || t('tagSelect')}</span><ChevronDown size={15} />
    </button>
    {open && <div className={styles.tagPopover}>
      <div className={styles.searchField}><Search size={15} /><input ref={input} role="combobox" aria-label={t('tagSearch')} aria-autocomplete="list" aria-expanded="true" aria-controls={listId} value={search} placeholder={t('tagSearch')} onChange={event => setSearch(event.target.value)} onKeyDown={event => {
        if (event.key === 'Enter') event.preventDefault();
        if (event.key === 'ArrowDown') { event.preventDefault(); container.current?.querySelector<HTMLButtonElement>('[role="option"]')?.focus(); }
      }} /></div>
      <div id={listId} role="listbox" aria-label={t('tags')} aria-multiselectable={multiple || undefined} className={styles.tagOptions} onKeyDown={event => {
        const options = Array.from(event.currentTarget.querySelectorAll<HTMLButtonElement>('[role="option"]'));
        const index = options.indexOf(document.activeElement as HTMLButtonElement);
        let next = index;
        if (event.key === 'ArrowDown') next = Math.min(index + 1, options.length - 1);
        else if (event.key === 'ArrowUp') next = Math.max(index - 1, 0);
        else if (event.key === 'Home') next = 0;
        else if (event.key === 'End') next = options.length - 1;
        else return;
        event.preventDefault(); options[next]?.focus();
      }}>
        {filtered.map(tag => <button key={tag.id} type="button" role="option" aria-selected={value.includes(tag.name)} onClick={() => choose(tag.name)}>
          <span>{tag.name}</span>{value.includes(tag.name) && <Check size={15} />}
        </button>)}
        {!filtered.length && <p className={styles.menuEmpty}>{t('noTags')}</p>}
      </div>
      <p className={styles.menuHint}>{t('tagOnlyExisting')}</p>
    </div>}
  </div>;
}
