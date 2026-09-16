import { describe, expect, it, vi } from 'vitest';
import { renderHook, waitFor } from '@testing-library/react';
import { createElement } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { SSEDecoder, useLiveEvents } from './events';
import { configureAPI } from './api';
describe('authenticated event stream', () => {
  it('parses fragmented CRLF and multi-line payloads without emitting comments', () => {
    const parser = new SSEDecoder(); expect(parser.push(': heartbeat\r\n\r\nid: 42\r\nevent: user\r\ndata: one\r')).toEqual([]);
    expect(parser.push('\ndata: two\r\n\r\n')).toEqual([{ id: '42', type: 'user', data: 'one\ntwo' }]);
  });
  it('reconnects with Last-Event-ID and invalidates widget and review on reconnection', async () => {
    configureAPI('http://127.0.0.1:7331', 'test-only');
    const fetch = vi.fn().mockImplementationOnce(async () => new Response(new ReadableStream({ start(controller) { controller.enqueue(new TextEncoder().encode('id: 7\nevent: user\ndata: {}\n\n')); controller.close(); } }))).mockImplementation(async () => new Response(new ReadableStream({ start() {} })));
    vi.stubGlobal('fetch', fetch);
    const client = new QueryClient(); const invalidate = vi.spyOn(client, 'invalidateQueries');
    const hook = renderHook(() => useLiveEvents(), { wrapper: ({ children }) => createElement(QueryClientProvider, { client }, children) });
    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(2), { timeout: 2500 });
    expect(fetch.mock.calls[1][1].headers.get('Last-Event-ID')).toBe('7');
    expect(fetch.mock.calls[1][1].headers.get('Authorization')).toBe('Bearer test-only');
    expect(invalidate.mock.calls.filter(call => call[0]?.queryKey?.[0] === 'ui-state').length).toBeGreaterThanOrEqual(2);
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ['desktop'] }); hook.unmount(); vi.unstubAllGlobals();
  });
});
