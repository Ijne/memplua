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
declare global { interface Window { __MEMPLUA_DESKTOP__?: DesktopBridge; _wails?: unknown; } }

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
export const isDesktop = () => Boolean(window.__MEMPLUA_DESKTOP__ || window._wails);
export const bootstrap = () => call<Bootstrap>('Bootstrap');
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
