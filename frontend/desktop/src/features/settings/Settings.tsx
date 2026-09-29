import { useEffect, useRef, useState, type ReactNode } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { Check, ChevronDown, CircleHelp, FolderOpen, Globe2, LoaderCircle, Monitor, Moon, Settings2, SlidersHorizontal, Sun } from 'lucide-react';
import { api } from '../../lib/api';
import { isDesktop, pickFile, pickFolder, setAlwaysOnTop, setAutostart } from '../../lib/bridge';
import { applyTheme } from '../../lib/preferences';
import type { Settings as SettingsData } from '../../lib/types';
import { Failure, InlineError, Loading } from '../../components/Feedback';
import styles from './Settings.module.css';

import { FileHelp, type FileHelpKey } from './FileHelp';

import { formatDuration, validDuration } from '../../lib/duration';

const durationKeys = ['model_startup_timeout', 'model_response_header_timeout', 'model_stream_idle_timeout', 'model_request_timeout', 'audio_transcription_timeout', 'conspect_max_duration', 'conspect_idle_timeout'] as const;
type Draft = Record<typeof durationKeys[number], string> & { ui_language: string; conspect_language: string; theme: string; always_on_top: boolean; start_with_windows: boolean; obsidian_directory: string; auto_export: boolean; models_managed: boolean; llama_binary: string; llm_model: string; whisper_model: string; silero_model: string; onnx_runtime: string; llm_url: string; model_parallel: number; model_gpu_layers: number; llama_server_args: string; log_level: string; processing_limit: number; review_limit: number };
export function settingsDraft(data: SettingsData): Draft {
  return { model_startup_timeout: formatDuration(data.models.startup_timeout), model_response_header_timeout: formatDuration(data.models.response_header_timeout), model_stream_idle_timeout: formatDuration(data.models.stream_idle_timeout), model_request_timeout: formatDuration(data.models.request_timeout), audio_transcription_timeout: formatDuration(data.audio.transcription_timeout), conspect_max_duration: formatDuration(data.pipeline.conspect_max_duration), conspect_idle_timeout: formatDuration(data.pipeline.conspect_idle_timeout), ui_language: data.ui.language, conspect_language: data.conspect.language, theme: data.ui.theme, always_on_top: data.ui.always_on_top, start_with_windows: data.ui.start_with_windows, obsidian_directory: data.export.obsidian_directory, auto_export: data.export.auto, models_managed: data.models.managed, llama_binary: data.models.llama_binary, llm_model: data.models.llm_model, whisper_model: data.models.whisper_model, silero_model: data.models.silero_model, onnx_runtime: data.models.onnx_runtime, llm_url: data.models.llm_url, model_parallel: data.models.parallel, model_gpu_layers: data.models.gpu_layers, llama_server_args: (data.models.server_args || []).join('\n'), log_level: data.logging.level, processing_limit: data.pipeline.processing_limit, review_limit: data.pipeline.review_limit };
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
  const [help, setHelp] = useState<FileHelpKey | null>(null);
  const helpOpener = useRef<HTMLButtonElement | null>(null);
  const [expanded, setExpanded] = useState({ llama: true, audio: false, processing: false, diagnostics: false });
  const dirty = JSON.stringify(base) !== JSON.stringify(draft);
  const invalidDuration = durationKeys.some((key) => !validDuration(draft[key]));
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
  function field(key: keyof Draft, type = 'text', hint?: string) {
    return <label>{t(key)}<input aria-label={t(key)} aria-describedby={hint ? `${key}-hint` : undefined} type={type} value={String(draft[key])} min={type === 'number' ? 1 : undefined} step={type === 'number' ? 1 : undefined} onChange={(event) => change(key, type === 'number' ? (event.target.value === '' ? 0 : Number(event.target.value)) : event.target.value)} required={type === 'number' || key === 'llm_url'} />{hint && <small id={`${key}-hint`}>{t(hint)}</small>}</label>;
  }
  function durationField(key: typeof durationKeys[number]) {
    const invalid = !validDuration(draft[key]);
    return <label><span>{t(key)}</span><input aria-label={t(key)} aria-describedby={`${key}-hint${invalid ? ` ${key}-error` : ''}`} aria-invalid={invalid} value={draft[key]} placeholder="90s, 10m" required spellCheck={false} onChange={(event) => change(key, event.target.value)} /><small id={`${key}-hint`}>{t(`${key}Hint`)}</small>{invalid && <small id={`${key}-error`} role="alert">{t('durationError')}</small>}</label>;
  }
  function pathField(key: 'obsidian_directory' | FileHelpKey) {
    const title = t(key === 'obsidian_directory' ? 'obsidianDirectory' : key);
    return <div className={styles.fileField}>
      <label htmlFor={`settings-${key}`}>{title}</label>
      <div className={styles.path}>
        <input id={`settings-${key}`} aria-describedby={key === 'obsidian_directory' ? undefined : `${key}-hint`} value={draft[key]} onChange={(event) => change(key, event.target.value)} spellCheck={false} />
        <button type="button" aria-label={`${t('browse')}: ${title}`} title={isDesktop() ? t('browse') : t('nativeOnly')} disabled={!isDesktop()} onClick={() => void browse(key, key === 'obsidian_directory')}><FolderOpen /></button>
        {key !== 'obsidian_directory' && <button type="button" aria-label={t('fileHelp', { name: title })} onClick={(event) => { helpOpener.current = event.currentTarget; setHelp(key); }}><CircleHelp /></button>}
      </div>
      {key !== 'obsidian_directory' && <small id={`${key}-hint`}>{t(`${key}Hint`)}</small>}
    </div>;
  }
  function technicalSection(key: keyof typeof expanded, title: string, durations: readonly (typeof durationKeys[number])[], children: ReactNode) {
    const invalid = durations.some((field) => !validDuration(draft[field]));
    return <section className={styles.technicalSection}>
      <h3><button type="button" id={`section-${key}`} aria-expanded={expanded[key]} aria-controls={`content-${key}`} onClick={() => setExpanded((current) => ({ ...current, [key]: !current[key] }))}>
        <span>{t(title)}{invalid && <small className={styles.validation}>{t('sectionDurationError')}</small>}</span><ChevronDown />
      </button></h3>
      <div id={`content-${key}`} role="region" aria-labelledby={`section-${key}`} hidden={!expanded[key]} className={styles.sectionContent}>{children}</div>
    </section>;
  }
  function toggle(key: 'always_on_top' | 'start_with_windows' | 'auto_export' | 'models_managed', title: string, hint?: string) {
    return <label className={styles.toggle}><span><span>{t(title)}</span>{hint && <small>{t(hint)}</small>}</span><input type="checkbox" role="switch" checked={draft[key]} disabled={(key === 'always_on_top' || key === 'start_with_windows') && !isDesktop()} onChange={(event) => change(key, event.target.checked)} /></label>;
  }
  return <>
  <form inert={help !== null} className={styles.page} onSubmit={(event) => { event.preventDefault(); if (dirty && !invalidDuration && !mutation.isPending) mutation.mutate(); }}>
    <header className={styles.heading}><div className={styles.headingIcon}><Settings2 /></div><div><h1>{t('settings')}</h1><p>{t('settingsIntro')}</p></div></header>
    <div className={styles.tabs} role="tablist" aria-label={t('settings')}>{[['basic', Globe2], ['developer', SlidersHorizontal]].map(([name, Icon]) => { const key = String(name); const TabIcon = Icon as typeof Globe2; return <button type="button" role="tab" id={`tab-${key}`} aria-controls={`panel-${key}`} aria-selected={tab === key} tabIndex={tab === key ? 0 : -1} key={key} onClick={() => setTab(key)} onKeyDown={(event) => { if (event.key === 'ArrowLeft' || event.key === 'ArrowRight') { event.preventDefault(); const tabs = ['basic', 'developer'], next = tabs[(tabs.indexOf(tab) + (event.key === 'ArrowRight' ? 1 : tabs.length - 1)) % 3]; setTab(next); document.getElementById(`tab-${next}`)?.focus(); } }}><TabIcon />{t(key)}</button>; })}</div>
    <div className={styles.body}>
      <fieldset disabled={mutation.isPending} className={styles.fieldset}>
        <section role="tabpanel" id="panel-basic" aria-labelledby="tab-basic" hidden={tab !== 'basic'} className={styles.panel}>
          <h3>{t('appearance')}</h3>
          <div className={styles.pair}><label>{t('language')}<select value={draft.ui_language} onChange={(event) => change('ui_language', event.target.value)}><option value="ru">Русский</option><option value="en">English</option></select></label><label>{t('conspectLanguage')}<select value={draft.conspect_language} onChange={(event) => change('conspect_language', event.target.value)}><option value="ru">Русский</option><option value="en">English</option></select></label></div>
          <div><label id="theme-label">{t('theme')}</label><div className={styles.themes} role="group" aria-labelledby="theme-label">{[['system', Monitor], ['light', Sun], ['dark', Moon]].map(([name, Icon]) => { const key = String(name), ThemeIcon = Icon as typeof Sun; return <button type="button" key={key} aria-pressed={draft.theme === key} onClick={() => change('theme', key)}><ThemeIcon />{t(key)}{draft.theme === key && <Check />}</button>; })}</div></div>
          <div className={styles.switches}>{toggle('always_on_top', 'alwaysOnTop', 'alwaysOnTopHint')}{toggle('start_with_windows', 'autostart', 'autostartHint')}</div>
          <div className={styles.divider} /><h3>{t('obsidian')}</h3>{pathField('obsidian_directory')}{toggle('auto_export', 'autoExport', 'autoExportHint')}
        </section>
        <section role="tabpanel" id="panel-developer" aria-labelledby="tab-developer" hidden={tab !== 'developer'} className={styles.panel}>
          <p className="muted">{t('developerIntro')}</p>
          {technicalSection('llama', 'llamaSection', durationKeys.slice(0, 4), <>
            {toggle('models_managed', 'managed', 'managedHint')}
            <p className="muted">{t(draft.models_managed ? 'managedModeHint' : 'externalModeHint')}</p>
            {pathField('llama_binary')}{pathField('llm_model')}
            {field('llm_url', 'url', 'llmURLHint')}
            <div className={styles.pair}>{field('model_parallel', 'number', 'parallelHint')}
              <label><span>{t('model_gpu_layers')}</span><input aria-label={t('model_gpu_layers')} aria-describedby="gpu-layers-hint" type="number" value={draft.model_gpu_layers} min={0} step={1} onChange={(event) => change('model_gpu_layers', event.target.value === '' ? 0 : Number(event.target.value))} required /><small id="gpu-layers-hint">{t('gpuHelpHint')}</small></label>
            </div>
            <label><span>{t('llama_server_args')}</span><textarea aria-label={t('llama_server_args')} aria-describedby="server-args-hint" value={draft.llama_server_args} onChange={(event) => change('llama_server_args', event.target.value)} placeholder={'--flash-attn\non'} spellCheck={false} /><small id="server-args-hint">{t('serverArgsHelpHint')}</small></label>
            <h3>{t('llmTimeouts')}</h3>{durationKeys.slice(0, 4).map((key) => <div key={key}>{durationField(key)}</div>)}
          </>)}
          {technicalSection('audio', 'audioSection', ['audio_transcription_timeout'], <>
            {pathField('whisper_model')}{pathField('silero_model')}{pathField('onnx_runtime')}{durationField('audio_transcription_timeout')}
          </>)}
          {technicalSection('processing', 'processingSection', ['conspect_max_duration', 'conspect_idle_timeout'], <>
            {durationField('conspect_max_duration')}{durationField('conspect_idle_timeout')}
            <div className={styles.pair}>{field('processing_limit', 'number', 'processingLimitHint')}{field('review_limit', 'number', 'reviewLimitHint')}</div>
          </>)}
          {technicalSection('diagnostics', 'diagnosticsSection', [], <>
            <label><span>{t('log_level')}</span><select aria-label={t('log_level')} aria-describedby="log-level-hint" value={draft.log_level} onChange={(event) => change('log_level', event.target.value)}>{['debug', 'info', 'warn', 'error'].map((value) => <option key={value}>{value}</option>)}</select><small id="log-level-hint">{t('logLevelHint')}</small></label>
          </>)}
        </section>
      </fieldset>
      {(mutation.isError || pickerError) && <InlineError />}
      {invalidDuration && <p className={styles.validation} role="status">{t('settingsDurationError')}</p>}
      {mutation.data?.restart_required && <p className={styles.restart} role="status">{t('restart', { keys: mutation.data.restart_keys.map((key) => t(({ 'models.parallel': 'model_parallel', 'models.gpu_layers': 'model_gpu_layers', 'models.server_args': 'llama_server_args', 'models.managed': 'managed', 'export.obsidian_directory': 'obsidianDirectory', 'export.auto': 'autoExport', 'logging.level': 'log_level', 'models.startup_timeout': 'model_startup_timeout', 'models.response_header_timeout': 'model_response_header_timeout', 'models.stream_idle_timeout': 'model_stream_idle_timeout', 'models.request_timeout': 'model_request_timeout', 'audio.transcription_timeout': 'audio_transcription_timeout', 'pipeline.conspect_max_duration': 'conspect_max_duration', 'pipeline.conspect_idle_timeout': 'conspect_idle_timeout' } as Record<string, string>)[key] || key.split('.').at(-1) || key, { defaultValue: t('advanced') })).join(', ') })}</p>}
    </div>
    <footer className={styles.footer}><span role="status">{mutation.isSuccess ? <><Check />{t('saved')}</> : dirty ? t('unsavedSettings') : t('savedSettingsHint')}</span><button type="button" disabled={!dirty || mutation.isPending} onClick={() => { setDraft(base); applyTheme(base.theme); void i18n.changeLanguage(base.ui_language); mutation.reset(); }}>{t('cancel')}</button><button className="primary" disabled={!dirty || invalidDuration || mutation.isPending} type="submit">{mutation.isPending ? <LoaderCircle className="spin" /> : <Check />}{t('save')}</button></footer>
  </form>
  {help && <FileHelp field={help} returnFocus={helpOpener.current} onClose={() => setHelp(null)} />}
  </>;
}
