import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import './lib/i18n';
import './styles/global.css';
import { App } from './App';

document.documentElement.dataset.window = new URLSearchParams(location.search).get('window') || 'review';
createRoot(document.getElementById('root')!).render(<StrictMode><App /></StrictMode>);
