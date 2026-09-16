import { test, expect, type Page } from '@playwright/test';
import { conspect, settings, tags, uiState } from '../src/test/fixtures';
import type { Decision } from '../src/features/review/types';

async function connect(page: Page, language: 'en' | 'ru' = 'en', theme: 'light' | 'dark' = 'light') {
  const data = structuredClone(conspect), preferences = structuredClone(settings), state = structuredClone(uiState);
  preferences.ui.language = language; preferences.ui.theme = theme;
  let applied = false;
  await page.addInitScript(({ language }) => {
    window.__KC_DESKTOP__ = {
      Bootstrap: async () => ({ apiAddress: location.origin, token: 'browser-test-credential', locale: language, systemTheme: 'light', autostart: false }),
      OpenWindow: async (name, id) => { window.dispatchEvent(new CustomEvent('test:window', { detail: { name, id } })); },
      HideWindow: async () => {}, Quit: async () => {}, PickFile: async () => '', PickFolder: async () => '', SetAlwaysOnTop: async () => {}, SetAutostart: async () => {}, ResizeWidget: async () => {},
    };
  }, { language });
  await page.route('**/api/v1/**', async route => {
    const url = new URL(route.request().url()), path = url.pathname;
    expect(route.request().headers().authorization).toBe('Bearer browser-test-credential');
    expect(route.request().headers()['x-knowledgecrawler-client']).toBe('desktop');
    let value: unknown;
    if (path.endsWith('/events')) { await route.fulfill({ contentType: 'text/event-stream', body: ': connected\n\n' }); return; }
    if (path.endsWith('/ui/state')) value = state;
    else if (path.endsWith('/settings')) {
      if (route.request().method() === 'PATCH') { const patch = route.request().postDataJSON(); preferences.ui.language = patch.ui_language || preferences.ui.language; preferences.ui.theme = patch.theme || preferences.ui.theme; value = { settings: preferences, restart_keys: [], restart_required: false }; } else value = preferences;
    }
    else if (path.endsWith('/taxonomy/tag')) value = tags;
    else if (path.includes('/review/items/')) {
      const decision = route.request().postDataJSON() as Decision; const item = data.items.find(item => path.endsWith(item.id))!;
      Object.assign(item, decision, { status: 'resolved' }); data.status = data.items.every(item => item.status === 'resolved') ? 'ready_to_apply' : 'review_pending'; await route.fulfill({ status: 204 }); return;
    }
    else if (path.endsWith('/apply')) { applied = true; state.pending_conspects = 0; state.pending_items = 0; await route.fulfill({ status: 204 }); return; }
    else if (path.endsWith('/source-text')) value = { text: 'Reliable systems begin with deliberate trade-offs.\n\nEventual consistency lets replicas converge over time. A temporary difference between replicas is expected, not a failure.\n\nChoose a consistency model based on what the user is trying to do. A shopping cart and a payment have different needs.', source_kind: 'audio/microphone', started_at: '2026-09-12T08:30:00Z', ended_at: '2026-09-12T08:35:00Z' };
    else if (path.endsWith('/review/conspects')) value = applied ? [] : [data];
    else if (path.endsWith('/review/conspects/note-1')) value = data;
    else if (path.endsWith('/graph')) value = { revision: 1, terms: [{ id: 'canonical-1', ...data.items[0].canonical_matches![0].value }], thoughts: [{ id: 'thought-1', ...data.items[1].final_value }], links: [{ term_id: 'canonical-1', thought_id: 'thought-1' }] };
    else if (path.endsWith('/start')) { state.sources[0].state = 'recording'; state.sources[0].session_id = 'session-1'; state.sources[0].actions = ['pause', 'stop']; value = {}; }
    else { await route.fulfill({ status: 404, json: { code: 'request.not_found' } }); return; }
    await route.fulfill({ json: value });
  });
}

for (const language of ['en', 'ru'] as const) for (const theme of ['light', 'dark'] as const) for (const scale of [1, 1.25, 1.5, 2]) {
  test(`review ${language} ${theme} DPI ${scale * 100}`, async ({ browser }) => {
    const context = await browser.newContext({ viewport: { width: 1180, height: 760 }, deviceScaleFactor: scale, locale: language, colorScheme: theme, reducedMotion: 'reduce', timezoneId: 'Europe/Moscow' });
    const page = await context.newPage(); await connect(page, language, theme);
    const errors: string[] = []; page.on('pageerror', error => errors.push(error.message));
    await page.goto('http://127.0.0.1:5173/?window=review');
    await expect(page.getByRole('textbox', { name: language === 'en' ? 'Name' : 'Название', exact: true })).toBeVisible();
    await expect(page.locator('html')).toHaveAttribute('data-theme', theme);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    await expect(page).toHaveScreenshot(`review-${language}-${theme}-${scale}.png`);
    expect(errors).toEqual([]); await context.close();
  });
}
test('review saves before keyboard navigation and applies only from summary', async ({ page }) => {
  await connect(page); await page.goto('/?window=review');
  await page.getByRole('textbox', { name: 'Name', exact: true }).fill('A clearer concept');
  await page.keyboard.press('Alt+ArrowRight');
  await expect(page.getByRole('textbox', { name: 'Thesis' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Apply conspect' })).toHaveCount(0);
  await page.getByRole('button', { name: 'Reject', exact: true }).click();
  await page.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Ready to add to your knowledge' })).toBeVisible();
  await expect(page).toHaveScreenshot('review-summary.png');
  await page.getByRole('button', { name: 'Apply conspect', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'All caught up' })).toBeVisible();
  expect(await page.evaluate(() => JSON.stringify({ ...localStorage, ...sessionStorage }))).not.toContain('browser-test-credential');
});
test('settings language, theme and keyboard tabs; source search; graph browsing', async ({ page }) => {
  await page.setViewportSize({ width: 760, height: 700 }); await connect(page); await page.goto('/?window=settings');
  await expect(page.getByRole('heading', { name: 'Settings', exact: true })).toBeVisible();
  await page.getByRole('combobox', { name: 'Interface language' }).selectOption('ru');
  await expect(page.getByRole('heading', { name: 'Настройки', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Тёмное', exact: true }).click();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  await expect(page).toHaveScreenshot('settings-ru-dark.png');
  await page.getByRole('button', { name: 'Сохранить изменения' }).click();
  await page.getByRole('tab', { name: 'Основные' }).focus(); await page.keyboard.press('ArrowRight');
  await expect(page.getByRole('tab', { name: 'Модели' })).toBeFocused();
  await page.setViewportSize({ width: 800, height: 640 }); await page.goto('/?window=source&conspect=note-1');
  await page.getByRole('searchbox').fill('consistency'); await expect(page.locator('mark')).toHaveCount(2);
  await expect(page).toHaveScreenshot('source-ru-dark.png');
  await page.setViewportSize({ width: 1100, height: 720 }); await page.goto('/?window=graph'); await expect(page.getByRole('heading', { name: 'Граф знаний' })).toBeVisible();
  await expect(page).toHaveScreenshot('graph-ru-dark.png');
});
test('minimum review window keeps actions reachable with reduced motion', async ({ page }) => {
  await page.setViewportSize({ width: 960, height: 640 }); await page.emulateMedia({ reducedMotion: 'reduce' }); await connect(page, 'ru', 'dark'); await page.goto('/?window=review');
  await expect(page.getByRole('textbox', { name: 'Название', exact: true })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await expect(page.getByRole('button', { name: 'Далее', exact: true }).last()).toBeInViewport();
  await expect(page).toHaveScreenshot('review-minimum-ru-dark.png');
});
test('widget source controls expand right and move the following controls', async ({ page }) => {
  await page.setViewportSize({ width: 475, height: 48 }); await connect(page); await page.goto('/?window=widget');
  const sourceButton = page.getByRole('button', { name: 'Microphone: Off', exact: true });
  const nextSource = page.getByRole('button', { name: 'System audio: Off', exact: true });
  const sourceBefore = await sourceButton.boundingBox();
  const nextBefore = await nextSource.boundingBox();
  await sourceButton.hover();
  const microphone = page.getByLabel('Microphone', { exact: true });
  await expect(microphone).toHaveCSS('width', '46px');
  const startButton = microphone.getByRole('button', { name: 'Start recording' });
  await expect(startButton).toBeVisible();
  const sourceExpanded = await sourceButton.boundingBox();
  const actionExpanded = await startButton.boundingBox();
  const nextExpanded = await nextSource.boundingBox();
  expect(sourceBefore).not.toBeNull(); expect(nextBefore).not.toBeNull();
  expect(sourceExpanded?.x).toBe(sourceBefore?.x);
  expect(actionExpanded!.x).toBeGreaterThan(sourceExpanded!.x + sourceExpanded!.width - 1);
  expect(nextExpanded!.x - nextBefore!.x).toBeCloseTo(46, 0);
  await expect(page.locator('[role="toolbar"]')).toHaveScreenshot('widget-source.png');
  await startButton.click();
  await expect(page.getByRole('button', { name: 'Microphone: Recording' })).toBeVisible();
  await page.getByRole('button', { name: 'Microphone: Recording' }).hover();
  await expect(microphone).toHaveCSS('width', '112px');
  await expect(microphone.getByRole('button', { name: 'Pause', exact: true })).toBeVisible();
  await expect(microphone.getByRole('button', { name: 'Stop', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Settings', exact: true }).hover();
  await expect(microphone).toHaveCSS('width', /^(0|1)px$/);
  await expect(microphone).toHaveCSS('opacity', '0');
  const nextCollapsed = await nextSource.boundingBox();
  expect(nextCollapsed?.x).toBe(nextBefore?.x);
  await expect(page.getByRole('button', { name: 'Quit KnowledgeCrawler', exact: true })).toBeVisible();
});
