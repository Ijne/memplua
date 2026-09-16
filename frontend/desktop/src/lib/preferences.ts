import { useEffect } from 'react';
import { useQuery } from '@tanstack/react-query';
import { api } from './api';
import type { Settings } from './types';
import i18n from './i18n';
export function applyTheme(theme: string) {
  document.documentElement.dataset.theme = theme === 'system' ? (matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light') : theme;
}
export function usePreferences() {
  const query = useQuery({ queryKey: ['settings'], queryFn: ({ signal }) => api<Settings>('/api/v1/settings', { signal }), staleTime: 15_000 });
  useEffect(() => {
    const theme = query.data?.ui.theme || 'system';
    applyTheme(theme);
    const media = matchMedia('(prefers-color-scheme: dark)');
    const update = () => applyTheme(theme);
    media.addEventListener('change', update);
    return () => media.removeEventListener('change', update);
  }, [query.data?.ui.theme]);
  useEffect(() => { if (query.data?.ui.language) void i18n.changeLanguage(query.data.ui.language); }, [query.data?.ui.language]);
  useEffect(() => {
    const update = () => { document.documentElement.lang = i18n.resolvedLanguage || 'en'; };
    update(); i18n.on('languageChanged', update); return () => { i18n.off('languageChanged', update); };
  }, []);
  return query;
}
