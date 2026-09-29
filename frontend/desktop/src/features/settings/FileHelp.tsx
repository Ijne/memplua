import { useEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { useTranslation } from 'react-i18next';
import { ExternalLink, X } from 'lucide-react';
import { isDesktop, openExternalURL } from '../../lib/bridge';
import styles from './Settings.module.css';

const sources = {
  llama_binary: [['llamaReleases', 'https://github.com/ggml-org/llama.cpp/releases'], ['llamaArguments', 'https://github.com/ggml-org/llama.cpp/blob/master/tools/server/README.md']],
  llm_model: [['huggingFaceModels', 'https://huggingface.co/models'], ['mempluaModels', 'https://huggingface.co/ijne/memplua/tree/main']],
  whisper_model: [['whisperModels', 'https://huggingface.co/ggerganov/whisper.cpp']],
  silero_model: [['sileroModelFile', 'https://github.com/snakers4/silero-vad/blob/master/src/silero_vad/data/silero_vad.onnx']],
  onnx_runtime: [['onnxInstallation', 'https://onnxruntime.ai/docs/install/'], ['onnxReleases', 'https://github.com/microsoft/onnxruntime/releases']],
} as const;
const examples = {
  llama_binary: 'C:\\memplua\\runtime\\llama-server.exe',
  llm_model: 'C:\\memplua\\models\\knowledge-model.gguf',
  whisper_model: 'C:\\memplua\\models\\ggml-small.bin',
  silero_model: 'C:\\memplua\\silero-vad\\src\\silero_vad\\data\\silero_vad.onnx',
  onnx_runtime: 'C:\\memplua\\runtime\\onnxruntime.dll',
};
export type FileHelpKey = keyof typeof sources;

export function FileHelp({ field, returnFocus, onClose }: { field: FileHelpKey; returnFocus?: HTMLElement | null; onClose: () => void }) {
  const { t } = useTranslation();
  const dialog = useRef<HTMLDivElement>(null);
  const closeButton = useRef<HTMLButtonElement>(null);
  const [linkError, setLinkError] = useState(false);
  useEffect(() => {
    // The background may already be inert, so use the button captured on click.
    const opener = returnFocus || document.activeElement as HTMLElement | null;
    const overflow = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    closeButton.current?.focus();
    return () => { document.body.style.overflow = overflow; opener?.focus(); };
  }, [returnFocus]);
  return createPortal(<div className={styles.helpBackdrop}>
    <div ref={dialog} className={styles.helpDialog} role="dialog" aria-modal="true" aria-labelledby="file-help-title" aria-describedby="file-help-body" onKeyDown={(event) => {
      if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); onClose(); }
      if (event.key === 'Tab') {
        const controls = Array.from(dialog.current?.querySelectorAll<HTMLElement>('button:not(:disabled), a[href]') || []);
        const first = controls[0], last = controls.at(-1);
        if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus(); }
        else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus(); }
      }
    }}>
      <header><h2 id="file-help-title">{t(field)}</h2><button ref={closeButton} type="button" aria-label={t('close')} onClick={onClose}><X /></button></header>
      <p id="file-help-body">{t(`${field}Help`)}</p>
      <div className={styles.helpExample}><span>{t('exampleFilePath')}</span><code>{examples[field]}</code></div>
      <nav aria-label={t('helpSources')}>{sources[field].map(([label, url]) => <a key={url} href={url} target="_blank" rel="noopener noreferrer" onClick={(event) => {
        if (!isDesktop()) return;
        event.preventDefault(); setLinkError(false);
        void openExternalURL(url).catch(() => setLinkError(true));
      }}>{t(label)}<ExternalLink /></a>)}</nav>
      {linkError && <p className={styles.validation} role="alert">{t('helpLinkError')}</p>}
    </div>
  </div>, document.body);
}
