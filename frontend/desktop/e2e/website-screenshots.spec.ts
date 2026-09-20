import { test, expect, type Page } from '@playwright/test';
import { fileURLToPath } from 'node:url';
import { conspect, settings, tags, uiState } from '../src/test/fixtures';

const output = fileURLToPath(new URL('../../../website/assets/', import.meta.url));
const website = new URL('../../../website/index.html', import.meta.url).href;
const graph = {
  revision: 12,
  terms: [
    { id: 'eventual', name: 'Eventual consistency', description: 'A model in which replicas converge after updates stop.', tags: ['Distributed systems', 'Architecture'] },
    { id: 'availability', name: 'Availability', description: 'The system continues serving requests when components fail.', tags: ['Distributed systems', 'Reliability'] },
    { id: 'fault-tolerance', name: 'Fault tolerance', description: 'The ability to continue operating through partial failures.', tags: ['Reliability'] },
    { id: 'replication', name: 'Replication', description: 'Keeping copies of data on several nodes.', tags: ['Distributed systems'] },
    { id: 'idempotency', name: 'Idempotency', description: 'Repeating an operation has the same effect as applying it once.', tags: ['Architecture', 'Reliability'] },
    { id: 'cqrs', name: 'CQRS', description: 'Separate models for reading and changing state.', tags: ['Architecture'] },
    { id: 'backpressure', name: 'Backpressure', description: 'Consumers signal how much work they can safely accept.', tags: ['Distributed systems'] },
    { id: 'observability', name: 'Observability', description: 'Understanding internal state from system outputs.', tags: ['Reliability', 'Operations'] },
    { id: 'event-sourcing', name: 'Event sourcing', description: 'State is rebuilt from an ordered log of events.', tags: ['Architecture'] },
    { id: 'slo', name: 'Service level objective', description: 'A measurable reliability target for a service.', tags: ['Operations', 'Reliability'] },
  ],
  thoughts: [
    { id: 'thought-guarantees', thesis: 'Choose guarantees for the user’s task', body: 'A shopping cart can tolerate delay; a payment needs stronger guarantees.', tags: ['Architecture'] },
    { id: 'thought-retries', thesis: 'Safe retries need idempotency', body: 'Recovery becomes simpler when a repeated command cannot duplicate its effect.', tags: ['Reliability'] },
    { id: 'thought-replication', thesis: 'Replication trades freshness for resilience', body: 'Copies improve availability while making coordination explicit.', tags: ['Distributed systems'] },
    { id: 'thought-signals', thesis: 'Reliability needs visible signals', body: 'Objectives and observability turn incidents into measurable feedback.', tags: ['Operations'] },
    { id: 'thought-flow', thesis: 'Flow control protects the whole system', body: 'Backpressure prevents one slow consumer from becoming a cascading failure.', tags: ['Distributed systems'] },
    { id: 'thought-history', thesis: 'An event log preserves decisions', body: 'Event sourcing keeps the history required to explain how state changed.', tags: ['Architecture'] },
  ],
  links: [
    ['eventual', 'thought-guarantees'], ['cqrs', 'thought-guarantees'], ['idempotency', 'thought-retries'], ['fault-tolerance', 'thought-retries'],
    ['replication', 'thought-replication'], ['availability', 'thought-replication'], ['eventual', 'thought-replication'], ['observability', 'thought-signals'],
    ['slo', 'thought-signals'], ['backpressure', 'thought-flow'], ['availability', 'thought-flow'], ['event-sourcing', 'thought-history'], ['cqrs', 'thought-history'],
  ].map(([term_id, thought_id], index) => ({ id: `link-${index}`, term_id, thought_id })),
};

async function connect(page: Page) {
  const preferences = structuredClone(settings);
  preferences.ui.language = 'ru'; preferences.ui.theme = 'dark';
  const review = structuredClone(conspect);
  await page.addInitScript(() => {
    window.__MEMPLUA_DESKTOP__ = {
      Bootstrap: async () => ({ apiAddress: location.origin, token: 'website-capture', locale: 'ru', systemTheme: 'dark', autostart: false }),
      OpenWindow: async () => {}, HideWindow: async () => {}, Quit: async () => {}, PickFile: async () => '', PickFolder: async () => '',
      SetAlwaysOnTop: async () => {}, SetAutostart: async () => {}, ResizeWidget: async () => {},
    };
  });
  await page.route('**/api/v1/**', async route => {
    const path = new URL(route.request().url()).pathname;
    let value: unknown;
    if (path.endsWith('/events')) { await route.fulfill({ contentType: 'text/event-stream', body: ': connected\n\n' }); return; }
    if (path.endsWith('/ui/state')) value = uiState;
    else if (path.endsWith('/settings')) value = preferences;
    else if (path.endsWith('/taxonomy/tag')) value = tags;
    else if (path.endsWith('/review/conspects')) value = [review];
    else if (path.endsWith('/review/conspects/note-1')) value = review;
    else if (path.endsWith('/graph')) value = graph;
    else { await route.fulfill({ status: 404, json: { code: 'request.not_found' } }); return; }
    await route.fulfill({ json: value });
  });
}

test.skip(!process.env.WEBSITE_SCREENSHOTS, 'Run with WEBSITE_SCREENSHOTS=1 to refresh website assets.');
test('generate website interface screenshots', async ({ page }) => {
  await connect(page);
  await page.emulateMedia({ colorScheme: 'dark', reducedMotion: 'reduce' });

  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/?window=graph');
  await expect(page.getByRole('heading', { name: 'Memory' })).toBeVisible();
  await page.getByRole('button', { name: 'Термин: Eventual consistency' }).click();
  await page.screenshot({ path: `${output}interface-graph.png`, animations: 'disabled' });

  await page.setViewportSize({ width: 1240, height: 800 });
  await page.goto('/?window=review');
  await expect(page.getByRole('textbox', { name: 'Название', exact: true })).toBeVisible();
  await page.screenshot({ path: `${output}interface-review.png`, animations: 'disabled' });

  await page.setViewportSize({ width: 1000, height: 760 });
  await page.goto('/?window=settings');
  await expect(page.getByRole('heading', { name: 'Настройки', exact: true })).toBeVisible();
  await page.screenshot({ path: `${output}interface-settings.png`, animations: 'disabled' });
});

test('website renders the generated screenshots', async ({ page }, testInfo) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto(`${website}?lang=ru`);
  const section = page.locator('.screenshots');
  await section.scrollIntoViewIfNeeded();
  const images = section.locator('.screenshot-card img');
  await expect(images).toHaveCount(3);
  await expect.poll(() => images.evaluateAll(items => items.map(item => (item as HTMLImageElement).naturalWidth))).toEqual([1440, 1240, 1000]);
  await section.screenshot({ path: testInfo.outputPath('website-screenshots-section.png'), animations: 'disabled' });

  await page.setViewportSize({ width: 390, height: 844 });
  const [first, second] = await Promise.all([section.locator('.screenshot-card').nth(0).boundingBox(), section.locator('.screenshot-card').nth(1).boundingBox()]);
  expect(first).not.toBeNull(); expect(second).not.toBeNull();
  expect(second!.x).toBeCloseTo(first!.x, 0);
  expect(second!.y).toBeGreaterThan(first!.y + first!.height);
});
