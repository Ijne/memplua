import { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { Check, Cpu, FolderOpen, Globe2, LoaderCircle, Monitor, Moon, Settings2, SlidersHorizontal, Sun } from 'lucide-react';
import { api } from '../../lib/api';
import { isDesktop, pickFile, pickFolder, setAlwaysOnTop, setAutostart } from '../../lib/bridge';
import { applyTheme } from '../../lib/preferences';
import type { Settings as SettingsData } from '../../lib/types';
import { Failure, InlineError, Loading } from '../../components/Feedback';
import styles from './Settings.module.css';

type Draft = { ui_language: string; conspect_language: string; theme: string; always_on_top: boolean; start_with_windows: boolean; obsidian_directory: string; auto_export: boolean; models_managed: boolean; llama_binary: string; llm_model: string; whisper_model: string; silero_model: string; onnx_runtime: string; llm_url: string; model_parallel: number; model_gpu_layers: number; llama_server_args: string; log_level: string; processing_limit: number; review_limit: number };
export function settingsDraft(data: SettingsData): Draft {
  return { ui_language: data.ui.language, conspect_language: data.conspect.language, theme: data.ui.theme, always_on_top: data.ui.always_on_top, start_with_windows: data.ui.start_with_windows, obsidian_directory: data.export.obsidian_directory, auto_export: data.export.auto, models_managed: data.models.managed, llama_binary: data.models.llama_binary, llm_model: data.models.llm_model, whisper_model: data.models.whisper_model, silero_model: data.models.silero_model, onnx_runtime: data.models.onnx_runtime, llm_url: data.models.llm_url, model_parallel: data.models.parallel, model_gpu_layers: data.models.gpu_layers, llama_server_args: (data.models.server_args || []).join('\n'), log_level: data.logging.level, processing_limit: data.pipeline.processing_limit, review_limit: data.pipeline.review_limit };
}
export function Settings() {
  const query = useQuery({ queryKey: ['settings'], queryFn: ({ signal }) => api<SettingsData>('/api/v1/settings', { signal }) });
  if (!query.data) return query.isError ? <Failure retry={() => void query.refetch()} /> : <Loading />;
  return <SettingsForm initial={query.data} />;
}
function SettingsForm({ initial }: { initial: SettingsData }) {
  const { t, i18n } = useTranslation();
  const client = useQueryClient();
  const [base, setBase] = useState(() => settingsDraft(initial));
  const [draft, setDraft] = useState(base);
  const [tab, setTab] = useState('basic');
  const [pickerError, setPickerError] = useState(false);
  const dirty = JSON.stringify(base) !== JSON.stringify(draft);
  function change<K extends keyof Draft>(key: K, value: Draft[K]) {
    setDraft((current) => ({ ...current, [key]: value })); mutation.reset();
    if (key === 'ui_language') void i18n.changeLanguage(String(value));
    if (key === 'theme') applyTheme(String(value));
  }
  useEffect(() => {
    const beforeUnload = (event: BeforeUnloadEvent) => { if (dirty) event.preventDefault(); };
    window.addEventListener('beforeunload', beforeUnload); return () => window.removeEventListener('beforeunload', beforeUnload);
  }, [dirty]);
  const mutation = useMutation({
    mutationFn: async () => {
      const patch: Record<string, unknown> = Object.fromEntries(Object.entries(draft).filter(([key, value]) => base[key as keyof Draft] !== value));
      if ('llama_server_args' in patch) patch.llama_server_args = draft.llama_server_args.split(/\r?\n/).filter((line) => line.length);
      let topChanged = false, startChanged = false;
      try {
        if ('always_on_top' in patch) { await setAlwaysOnTop(draft.always_on_top); topChanged = true; }
        if ('start_with_windows' in patch) { await setAutostart(draft.start_with_windows); startChanged = true; }
        return await api<{ settings: SettingsData; restart_keys: string[]; restart_required: boolean }>('/api/v1/settings', { method: 'PATCH', body: JSON.stringify(patch) });
      } catch (error) {
        if (topChanged) await setAlwaysOnTop(base.always_on_top).catch(() => undefined);
        if (startChanged) await setAutostart(base.start_with_windows).catch(() => undefined);
        throw error;
      }
    },
    onSuccess: (response) => { const value = settingsDraft(response.settings); setBase(value); setDraft(value); client.setQueryData(['settings'], response.settings); },
  });
  const browse = async (key: keyof Draft, directory = false) => {
    setPickerError(false);
    try { const path = await (directory ? pickFolder : pickFile)(t(key === 'obsidian_directory' ? 'obsidianDirectory' : key)); if (path) change(key, path); } catch { setPickerError(true); }
  };
  function field(key: keyof Draft, type = 'text') {
    return <label>{t(key)}<input type={type} value={String(draft[key])} min={type === 'number' ? 1 : undefined} step={type === 'number' ? 1 : undefined} onChange={(event) => change(key, type === 'number' ? (event.target.value === '' ? 0 : Number(event.target.value)) : event.target.value)} required={type === 'number' || key === 'llm_url'} /></label>;
  }
  function pathField(key: 'obsidian_directory' | 'llama_binary' | 'llm_model' | 'whisper_model' | 'silero_model' | 'onnx_runtime') {
    return <label>{t(key === 'obsidian_directory' ? 'obsidianDirectory' : key)}<span className={styles.path}><input value={draft[key]} onChange={(event) => change(key, event.target.value)} spellCheck={false} /><button type="button" aria-label={`${t('browse')}: ${t(key === 'obsidian_directory' ? 'obsidianDirectory' : key)}`} title={isDesktop() ? t('browse') : t('nativeOnly')} disabled={!isDesktop()} onClick={() => void browse(key, key === 'obsidian_directory')}><FolderOpen /></button></span></label>;
  }
  function toggle(key: 'always_on_top' | 'start_with_windows' | 'auto_export' | 'models_managed', title: string, hint?: string) {
    return <label className={styles.toggle}><span><span>{t(title)}</span>{hint && <small>{t(hint)}</small>}</span><input type="checkbox" role="switch" checked={draft[key]} disabled={(key === 'always_on_top' || key === 'start_with_windows') && !isDesktop()} onChange={(event) => change(key, event.target.checked)} /></label>;
  }
  return <form className={styles.page} onSubmit={(event) => { event.preventDefault(); if (dirty && !mutation.isPending) mutation.mutate(); }}>
    <header className={styles.heading}><div className={styles.headingIcon}><Settings2 /></div><div><h1>{t('settings')}</h1><p>{t('settingsIntro')}</p></div></header>
    <div className={styles.tabs} role="tablist" aria-label={t('settings')}>{[['basic', Globe2], ['models', Cpu], ['advanced', SlidersHorizontal]].map(([name, Icon]) => { const key = String(name); const TabIcon = Icon as typeof Globe2; return <button type="button" role="tab" id={`tab-${key}`} aria-controls={`panel-${key}`} aria-selected={tab === key} tabIndex={tab === key ? 0 : -1} key={key} onClick={() => setTab(key)} onKeyDown={(event) => { if (event.key === 'ArrowLeft' || event.key === 'ArrowRight') { event.preventDefault(); const tabs = ['basic', 'models', 'advanced'], next = tabs[(tabs.indexOf(tab) + (event.key === 'ArrowRight' ? 1 : 2)) % 3]; setTab(next); document.getElementById(`tab-${next}`)?.focus(); } }}><TabIcon />{t(key)}</button>; })}</div>
    <div className={styles.body}>
      <fieldset disabled={mutation.isPending} className={styles.fieldset}>
        <section role="tabpanel" id="panel-basic" aria-labelledby="tab-basic" hidden={tab !== 'basic'} className={styles.panel}>
          <h3>{t('appearance')}</h3>
          <div className={styles.pair}><label>{t('language')}<select value={draft.ui_language} onChange={(event) => change('ui_language', event.target.value)}><option value="ru">Русский</option><option value="en">English</option></select></label><label>{t('conspectLanguage')}<select value={draft.conspect_language} onChange={(event) => change('conspect_language', event.target.value)}><option value="ru">Русский</option><option value="en">English</option></select></label></div>
          <div><label id="theme-label">{t('theme')}</label><div className={styles.themes} role="group" aria-labelledby="theme-label">{[['system', Monitor], ['light', Sun], ['dark', Moon]].map(([name, Icon]) => { const key = String(name), ThemeIcon = Icon as typeof Sun; return <button type="button" key={key} aria-pressed={draft.theme === key} onClick={() => change('theme', key)}><ThemeIcon />{t(key)}{draft.theme === key && <Check />}</button>; })}</div></div>
          <div className={styles.switches}>{toggle('always_on_top', 'alwaysOnTop', 'alwaysOnTopHint')}{toggle('start_with_windows', 'autostart', 'autostartHint')}</div>
          <div className={styles.divider} /><h3>{t('obsidian')}</h3>{pathField('obsidian_directory')}{toggle('auto_export', 'autoExport', 'autoExportHint')}
        </section>
        <section role="tabpanel" id="panel-models" aria-labelledby="tab-models" hidden={tab !== 'models'} className={styles.panel}><p className="muted">{t('modelIntro')}</p>{toggle('models_managed', 'managed')}{pathField('llama_binary')}{pathField('llm_model')}{pathField('whisper_model')}{pathField('silero_model')}{pathField('onnx_runtime')}</section>
        <section role="tabpanel" id="panel-advanced" aria-labelledby="tab-advanced" hidden={tab !== 'advanced'} className={styles.panel}><p className="muted">{t('advancedIntro')}</p>{field('llm_url', 'url')}<div className={styles.pair}>{field('model_parallel', 'number')}<label>{t('model_gpu_layers')}<input type="number" value={draft.model_gpu_layers} min={0} step={1} onChange={(event) => change('model_gpu_layers', event.target.value === '' ? 0 : Number(event.target.value))} required /><small>{t('gpuLayersHint')}</small></label></div><div className={styles.pair}><label>{t('log_level')}<select value={draft.log_level} onChange={(event) => change('log_level', event.target.value)}>{['debug', 'info', 'warn', 'error'].map((value) => <option key={value}>{value}</option>)}</select></label></div><label>{t('llama_server_args')}<textarea value={draft.llama_server_args} onChange={(event) => change('llama_server_args', event.target.value)} spellCheck={false} /><small>{t('argsHint')}</small></label><div className={styles.pair}>{field('processing_limit', 'number')}{field('review_limit', 'number')}</div></section>
      </fieldset>
      {(mutation.isError || pickerError) && <InlineError />}
      {mutation.data?.restart_required && <p className={styles.restart} role="status">{t('restart', { keys: mutation.data.restart_keys.map((key) => t(({ 'models.parallel': 'model_parallel', 'models.gpu_layers': 'model_gpu_layers', 'models.server_args': 'llama_server_args', 'models.managed': 'managed', 'export.obsidian_directory': 'obsidianDirectory', 'export.auto': 'autoExport', 'logging.level': 'log_level' } as Record<string, string>)[key] || key.split('.').at(-1) || key, { defaultValue: t('advanced') })).join(', ') })}</p>}
    </div>
    <footer className={styles.footer}><span role="status">{mutation.isSuccess ? <><Check />{t('saved')}</> : dirty ? t('unsavedSettings') : t('savedSettingsHint')}</span><button type="button" disabled={!dirty || mutation.isPending} onClick={() => { setDraft(base); applyTheme(base.theme); void i18n.changeLanguage(base.ui_language); mutation.reset(); }}>{t('cancel')}</button><button className="primary" disabled={!dirty || mutation.isPending} type="submit">{mutation.isPending ? <LoaderCircle className="spin" /> : <Check />}{t('save')}</button></footer>
  </form>;
}
