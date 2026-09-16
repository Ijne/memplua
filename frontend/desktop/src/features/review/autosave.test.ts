import { afterEach, describe, expect, it, vi } from 'vitest';
import { DraftAutosave } from './autosave';
afterEach(() => vi.useRealTimers());
describe('review draft persistence', () => {
  it('debounces edits into one save after 500ms', async () => {
    vi.useFakeTimers(); const save = vi.fn().mockResolvedValue(undefined); const draft = new DraftAutosave<string>('', save);
    draft.edit('a'); await vi.advanceTimersByTimeAsync(300); draft.edit('abc'); await vi.advanceTimersByTimeAsync(499);
    expect(save).not.toHaveBeenCalled(); await vi.advanceTimersByTimeAsync(1);
    expect(save).toHaveBeenCalledExactlyOnceWith('abc'); expect(draft.status).toBe('saved');
  });
  it('flushes immediately before navigation', async () => {
    const save = vi.fn().mockResolvedValue(undefined), draft = new DraftAutosave<string>('', save);
    draft.edit('latest'); expect(await draft.flush()).toBe(true); expect(save).toHaveBeenCalledExactlyOnceWith('latest');
  });
  it('serializes edits made while a request is in flight and waits for the newest revision', async () => {
    let release!: () => void;
    const save = vi.fn().mockImplementationOnce(() => new Promise<void>(resolve => { release = resolve; })).mockResolvedValue(undefined);
    const draft = new DraftAutosave<string>('', save); draft.edit('first'); const first = draft.flush(); draft.edit('second'); const second = draft.flush();
    expect(save).toHaveBeenCalledTimes(1); release(); expect(await first).toBe(true); expect(await second).toBe(true);
    expect(save.mock.calls.map(call => call[0])).toEqual(['first', 'second']); expect(draft.dirty).toBe(false);
  });
  it('preserves the draft on failure, blocks navigation and retries explicitly', async () => {
    const save = vi.fn().mockRejectedValueOnce(new Error('offline')).mockResolvedValue(undefined); const draft = new DraftAutosave<string>('', save);
    draft.edit('my work'); expect(await draft.flush()).toBe(false); expect(draft.value).toBe('my work'); expect(draft.dirty).toBe(true); expect(draft.status).toBe('failed');
    expect(await draft.flush()).toBe(true); expect(draft.dirty).toBe(false); expect(save).toHaveBeenCalledTimes(2);
  });
});
