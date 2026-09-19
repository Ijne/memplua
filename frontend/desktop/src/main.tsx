import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import './lib/i18n';
import './styles/global.css';
import { App } from './App';

const zoomKeys = new Set(['+', '-', '=', '0']);
window.addEventListener('keydown', event => {
  if ((event.ctrlKey || event.metaKey) && zoomKeys.has(event.key)) {
    event.preventDefault();
    event.stopPropagation();
  }
}, { capture: true });
window.addEventListener('wheel', event => {
  if (event.ctrlKey || event.metaKey) event.preventDefault();
}, { capture: true, passive: false });

document.documentElement.dataset.window = new URLSearchParams(location.search).get('window') || 'review';
createRoot(document.getElementById('root')!).render(<StrictMode><App /></StrictMode>);
