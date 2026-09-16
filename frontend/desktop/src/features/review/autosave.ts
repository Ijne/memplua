import { useEffect, useRef, useState } from 'react';

export type SaveStatus = 'idle' | 'waiting' | 'saving' | 'saved' | 'failed';

/** A single writer: changes made during a request are saved after that request. */
export class DraftAutosave<T> {
  private revision = 0;
  private savedRevision = 0;
  private timer: ReturnType<typeof setTimeout> | undefined;
  private flight: Promise<boolean> | undefined;
  private listeners = new Set<() => void>();
  status: SaveStatus = 'idle';
  error: unknown;

  constructor(public value: T, private persist: (value: T) => Promise<void>, private delay = 500) {}

  get dirty() { return this.revision !== this.savedRevision; }
  subscribe(listener: () => void) { this.listeners.add(listener); return () => { this.listeners.delete(listener); }; }
  private publish() { this.listeners.forEach(listener => listener()); }

  edit(value: T) {
    this.value = value;
    this.revision++;
    this.status = 'waiting';
    this.error = undefined;
    clearTimeout(this.timer);
    this.timer = setTimeout(() => { void this.flush(); }, this.delay);
    this.publish();
  }

  flush(): Promise<boolean> {
    clearTimeout(this.timer);
    if (this.flight) return this.flight;
    if (!this.dirty) return Promise.resolve(true);
    this.flight = this.drain().finally(() => { this.flight = undefined; });
    return this.flight;
  }

  private async drain(): Promise<boolean> {
    while (this.dirty) {
      const revision = this.revision;
      const snapshot = this.value;
      this.status = 'saving';
      this.publish();
      try {
        await this.persist(snapshot);
        this.savedRevision = revision;
        this.error = undefined;
      } catch (error) {
        this.status = 'failed';
        this.error = error;
        this.publish();
        return false;
      }
    }
    this.status = 'saved';
    this.publish();
    return true;
  }

  cancelTimer() { clearTimeout(this.timer); }
}

export function useDraftAutosave<T>(initial: T, persist: (value: T) => Promise<void>) {
  const callback = useRef(persist);
  callback.current = persist;
  const [saver] = useState(() => new DraftAutosave(initial, value => callback.current(value)));
  const [, render] = useState(0);
  useEffect(() => {
    const unsubscribe = saver.subscribe(() => render(value => value + 1));
    const beforeUnload = (event: BeforeUnloadEvent) => {
      if (saver.dirty) { event.preventDefault(); event.returnValue = ''; }
    };
    window.addEventListener('beforeunload', beforeUnload);
    return () => { unsubscribe(); saver.cancelTimer(); window.removeEventListener('beforeunload', beforeUnload); };
  }, [saver]);
  return saver;
}
