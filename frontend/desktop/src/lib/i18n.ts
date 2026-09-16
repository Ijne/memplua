import i18n from 'i18next';
import { initReactI18next } from 'react-i18next';
import { en, ru } from './locales';
import { en as reviewEN, ru as reviewRU } from '../features/review/locales';
void i18n.use(initReactI18next).init({
  resources: { en: { translation: en, review: reviewEN }, ru: { translation: ru, review: reviewRU } },
  lng: navigator.language.toLowerCase().startsWith('ru') ? 'ru' : 'en', fallbackLng: 'en',
  interpolation: { escapeValue: false }, returnNull: false,
});
export default i18n;
