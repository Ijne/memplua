// An isolated real-WebView2 smoke test. Never enables recording or autostart.
// Usage: node e2e/native-smoke.mjs <desktop-executable>
import { chromium } from '@playwright/test';
import { spawn } from 'node:child_process';
import { mkdir, writeFile, readFile } from 'node:fs/promises';
import { resolve, join } from 'node:path';
import { createServer } from 'node:net';
import assert from 'node:assert/strict';

const executable = resolve(process.argv[2] || '../../.cache/memplua-desktop.exe');
const directory = resolve('../../.cache/native-smoke', String(Date.now()));
await mkdir(directory, { recursive: true });
await writeFile(join(directory, 'config.toml'), '[models]\nmanaged = false\n[logging]\nconsole = false\n[ui]\nlanguage = "en"\nstart_with_windows = false\n');
async function freePort() { const server = createServer(); await new Promise(resolve => server.listen(0, '127.0.0.1', resolve)); const port = server.address().port; await new Promise(resolve => server.close(resolve)); return port; }
const apiPort = await freePort(), debugPort = await freePort();
const env = Object.fromEntries(Object.entries(process.env).filter(([key]) => !key.startsWith('MEMPLUA_')));
env.MEMPLUA_DESKTOP_DEBUG_PORT = String(debugPort);
const args = ['--config', join(directory, 'config.toml'), '--data-dir', directory, '--listen', `127.0.0.1:${apiPort}`];
const child = spawn(executable, args, { env, windowsHide: true, stdio: 'ignore' });
child.on('error', () => {});
const sleep = milliseconds => new Promise(resolve => setTimeout(resolve, milliseconds));
let browser, second;
async function until(fn, timeout = 20000) { const end = Date.now() + timeout; while (Date.now() < end) { const result = await fn().catch(() => undefined); if (result) return result; await sleep(150); } throw new Error('Native smoke timed out'); }
try {
  await until(async () => (await fetch(`http://127.0.0.1:${debugPort}/json/version`)).ok);
  browser = await chromium.connectOverCDP(`http://127.0.0.1:${debugPort}`);
  const context = browser.contexts()[0];
  const widget = await until(async () => context.pages().find(page => page.url().includes('window=widget')));
  await widget.getByRole('toolbar', { name: 'memplua controls' }).waitFor({ timeout: 20000 });
  assert.equal(await widget.getByRole('button', { name: 'Microphone: Off', exact: true }).count(), 1);
  const bridgeOK = await widget.evaluate(async () => {
    const { Call } = await import('/wails/runtime.js');
    const boot = await Call.ByName('crawler/internal/desktop.Shell.Bootstrap');
    const response = await fetch(`${boot.apiAddress}/api/v1/ui/state`, { headers: { Authorization: `Bearer ${boot.token}`, 'X-Memplua-Client': 'desktop' } });
    const state = await response.json();
    return response.ok && state.sources.every(source => source.state === 'off') && !JSON.stringify({ ...localStorage, ...sessionStorage }).includes(boot.token);
  });
  assert.equal(bridgeOK, true);
  await widget.screenshot({ path: join(directory, 'widget.png') });
  for (const name of ['review', 'settings', 'graph']) {
    await widget.evaluate(async name => { const { Call } = await import('/wails/runtime.js'); await Call.ByName('crawler/internal/desktop.Shell.OpenWindow', name, ''); }, name);
    const page = await until(async () => context.pages().find(page => page.url().includes(`window=${name}`)));
    await page.locator('h1').waitFor({ timeout: 15000 });
    assert.equal(await page.getByText('Connection interrupted', { exact: true }).count(), 0);
    await page.screenshot({ path: join(directory, `${name}.png`) });
    await page.evaluate(async () => { const { Window } = await import('/wails/runtime.js'); await Window.Close(); });
    assert.equal(page.isClosed(), false, 'Close must hide the singleton WebView');
    await widget.evaluate(async name => { const { Call } = await import('/wails/runtime.js'); await Call.ByName('crawler/internal/desktop.Shell.OpenWindow', name, ''); }, name);
    assert.equal(context.pages().filter(page => page.url().includes(`window=${name}`)).length, 1);
  }
  const pagesBefore = context.pages().length;
  second = spawn(executable, args, { env, windowsHide: true, stdio: 'ignore' });
  await until(async () => second.exitCode !== null, 7000);
  assert.equal(second.exitCode, 0, 'Second instance should activate the first and exit');
  assert.equal(context.pages().length, pagesBefore);
  await widget.evaluate(async () => { const { Window } = await import('/wails/runtime.js'); await Window.SetPosition(100, 32); });
  await sleep(700);
  await widget.evaluate(async () => { const { Application } = await import('/wails/runtime.js'); void Application.Quit(); }).catch(() => {});
  await until(async () => child.exitCode !== null, 10000);
  assert.equal(child.exitCode, 0, 'Quit must shut down the runtime cleanly');
  const state = JSON.parse(await readFile(join(directory, 'ui-state.json'), 'utf8'));
  assert.equal(state.windows.widget.width, 363);
  assert.equal(state.windows.widget.height, 48);
  console.log('Native smoke passed: real bridge, authenticated API, sources off, singleton windows, close/hide, second instance, position persistence, graceful Quit.');
} finally {
  if (second && second.exitCode === null) second.kill();
  if (child.exitCode === null) child.kill();
  await browser?.close().catch(() => {});
}
