export type WindowName = 'widget' | 'review' | 'settings' | 'source' | 'graph';
export interface Bootstrap { apiAddress: string; token: string; locale: string; systemTheme: 'light' | 'dark'; autostart: boolean }
export interface DesktopBridge {
  Bootstrap(): Promise<Bootstrap>;
  OpenWindow(name: WindowName, conspectID: string): Promise<void>;
  HideWindow(name: WindowName): Promise<void>;
  Quit(): Promise<void>;
  PickFile(title: string): Promise<string>;
  PickFolder(title: string): Promise<string>;
  SetAlwaysOnTop(value: boolean): Promise<void>;
  SetAutostart(value: boolean): Promise<void>;
  ResizeWidget(width: number, height: number): Promise<void>;
}
declare global { interface Window { __MEMPLUA_DESKTOP__?: DesktopBridge; _wails?: { flags?: unknown }; } }

// The optional adapter supports local browser integration tests. Credentials never
// enter URLs, build-time environment variables or browser storage.
async function call<T>(method: keyof DesktopBridge, ...args: unknown[]): Promise<T> {
  if (window.__MEMPLUA_DESKTOP__) {
    const fn = window.__MEMPLUA_DESKTOP__[method] as (...args: unknown[]) => Promise<T>;
    return fn(...args);
  }
  const { Call } = await import('@wailsio/runtime');
  return Call.ByName(`crawler/internal/desktop.Shell.${method}`, ...args) as Promise<T>;
}
export const isDesktop = () => Boolean(window.__MEMPLUA_DESKTOP__ || window.location.hostname === 'wails.localhost' || window._wails?.flags);

// Wails injects its native runtime after the page finishes navigating. The
// frontend can execute first, especially on a cold WebView2 startup.
async function waitForDesktopRuntime() {
  if (window.__MEMPLUA_DESKTOP__) return;
  const deadline = Date.now() + 15000;
  while (!window._wails?.flags) {
    if (Date.now() >= deadline) throw new Error('Desktop runtime did not become ready');
    await new Promise(resolve => setTimeout(resolve, 25));
  }
}

export const bootstrap = async () => {
  await waitForDesktopRuntime();
  return call<Bootstrap>('Bootstrap');
};
export async function openWindow(name: WindowName, conspectID = '') {
  if (isDesktop()) return call<void>('OpenWindow', name, conspectID);
  const url = new URL(window.location.href); url.search = new URLSearchParams({ window: name, ...(conspectID ? { conspect: conspectID } : {}) }).toString();
  window.open(url, `memplua-${name}`);
}
export const hideWindow = (name: WindowName) => call<void>('HideWindow', name);
export const pickFile = (title: string) => call<string>('PickFile', title);
export const pickFolder = (title: string) => call<string>('PickFolder', title);
export const setAlwaysOnTop = (value: boolean) => call<void>('SetAlwaysOnTop', value);
export const setAutostart = (value: boolean) => call<void>('SetAutostart', value);
export const resizeWidget = (width: number, height: number) => isDesktop() ? call<void>('ResizeWidget', width, height) : Promise.resolve();
export const quitApp = () => isDesktop() ? call<void>('Quit') : Promise.resolve();
