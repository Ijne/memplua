import { describe, expect, it } from 'vitest';
import { en, ru } from './locales';
import { en as reviewEN, ru as reviewRU } from '../features/review/locales';
function leaves(value: object, prefix = ''): Record<string, string> {
  return Object.fromEntries(Object.entries(value).flatMap(([key, val]) => typeof val === 'string' ? [[prefix + key, val]] : Object.entries(leaves(val, `${prefix}${key}.`))));
}
describe('translation coverage', () => {
  it.each([[en, ru], [reviewEN, reviewRU]])('covers every English key and interpolation in Russian', (english, russian) => {
    const a = leaves(english), b = leaves(russian); expect(Object.keys(b).sort()).toEqual(Object.keys(a).sort());
    for (const key of Object.keys(a)) { expect(b[key].trim()).not.toBe(''); expect(b[key].match(/{{.*?}}/g)?.sort()).toEqual(a[key].match(/{{.*?}}/g)?.sort()); }
  });
});
