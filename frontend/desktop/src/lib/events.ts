import { useEffect, useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { authenticatedFetch } from './api';

export interface StreamEvent { id: string; type: string; data: string }
// Handles fragmented UTF-8 (via TextDecoder), CRLF and multi-line SSE data.
export class SSEDecoder {
  private buffer = '';
  push(chunk: string): StreamEvent[] {
    this.buffer += chunk;
    const events: StreamEvent[] = [];
    let match: RegExpExecArray | null;
    while ((match = /\r?\n\r?\n/.exec(this.buffer))) {
      const block = this.buffer.slice(0, match.index);
      this.buffer = this.buffer.slice(match.index + match[0].length);
      const event = { id: '', type: 'message', data: '' };
      const data: string[] = [];
      for (const line of block.split(/\r?\n/)) {
        if (line.startsWith(':')) continue;
        const colon = line.indexOf(':');
        const field = colon < 0 ? line : line.slice(0, colon);
        const value = colon < 0 ? '' : line.slice(colon + 1).replace(/^ /, '');
        if (field === 'id' && !value.includes('\0')) event.id = value;
        if (field === 'event') event.type = value;
        if (field === 'data') data.push(value);
      }
      event.data = data.join('\n');
      if (data.length) events.push(event);
    }
    // A malformed server must not grow memory indefinitely.
    if (this.buffer.length > 1_048_576) throw new Error('Invalid event stream');
    return events;
  }
}

export function useLiveEvents() {
  const client = useQueryClient();
  const [connected, setConnected] = useState(false);
  useEffect(() => {
    const controller = new AbortController();
    let lastID = '', retry = 0;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const refresh = () => {
      for (const key of ['ui-state', 'desktop', 'review', 'conspects', 'settings', 'graph', 'taxonomy']) void client.invalidateQueries({ queryKey: [key] });
    };
    async function connect() {
      while (!controller.signal.aborted) {
        try {
          const response = await authenticatedFetch('/api/v1/events?view=desktop', { signal: controller.signal, headers: lastID ? { 'Last-Event-ID': lastID, Accept: 'text/event-stream' } : { Accept: 'text/event-stream' } });
          if (!response.ok || !response.body) throw new Error('Event stream unavailable');
          setConnected(true); retry = 0; refresh();
          const reader = response.body.getReader(), decoder = new TextDecoder(), parser = new SSEDecoder();
          try {
            while (!controller.signal.aborted) {
              const { done, value } = await reader.read();
              if (done) break;
              for (const event of parser.push(decoder.decode(value, { stream: true }))) {
                if (event.id) lastID = event.id;
                // Coalesce bursts. Events invalidate server state; raw event data
                // never becomes product copy or a second source of truth.
                if (!timer) timer = setTimeout(() => { timer = undefined; refresh(); }, 150);
              }
            }
          } finally { reader.releaseLock(); }
        } catch { /* Reconnect below; polling remains an independent fallback. */ }
        if (controller.signal.aborted) return;
        setConnected(false);
        await new Promise<void>((resolve) => {
          const delay = setTimeout(done, Math.min(30_000, 1000 * 2 ** retry++) + Math.random() * 300);
          function done() { clearTimeout(delay); controller.signal.removeEventListener('abort', done); resolve(); }
          controller.signal.addEventListener('abort', done, { once: true });
        });
      }
    }
    void connect();
    return () => { controller.abort(); clearTimeout(timer); };
  }, [client]);
  return connected;
}
