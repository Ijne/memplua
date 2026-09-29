import { describe, expect, it } from 'vitest';
import { formatDuration, parseSettingsJSON, validDuration } from './duration';

describe('settings durations', () => {
  it('preserves nanoseconds and large int64 values from wire JSON', () => {
    const value = parseSettingsJSON('{"models":{"request_timeout":9007199254740993},"text":"request_timeout"}') as { models: { request_timeout: string } };
    expect(value.models.request_timeout).toBe('9007199254740993');
    expect(formatDuration(value.models.request_timeout)).toBe('9007199254740993ns');
    expect(formatDuration(600000000000)).toBe('10m');
    expect(formatDuration(1234567891)).toBe('1234567891ns');
    expect(parseSettingsJSON('{"text":"\\\"request_timeout\\\":123","request_timeout":1}')).toEqual({ text: '\"request_timeout\":123', request_timeout: '1' });
  });
  it('accepts positive Go durations and rejects invalid or overflowing values', () => {
    for (const value of ['90s', '10m', '+1h30m', '0.000000001s', '.5m', '1.s', '9223372036854775807ns']) expect(validDuration(value), value).toBe(true);
    for (const value of ['', '0', '0s', '-1m', '90', '1d', '1m ', 'bad', '0.1ns', '9223372036854775808ns']) expect(validDuration(value), value).toBe(false);
  });
});
