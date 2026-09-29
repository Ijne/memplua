// GET settings uses nanoseconds. Keep arithmetic integral when displaying them.
export function formatDuration(nanoseconds: number | string): string {
  const value = BigInt(nanoseconds);
  for (const [unit, scale] of [['h', 3600000000000n], ['m', 60000000000n], ['s', 1000000000n]] as const) {
    if (value % scale === 0n) return `${value / scale}${unit}`;
  }
  return `${value}ns`;
}

// Preserve int64 duration digits before JSON.parse rounds large numeric values.
// The string-token alternative skips quoted text, including escaped quotes.
export function parseSettingsJSON(text: string): unknown {
  return JSON.parse(text.replace(/"(?:startup_timeout|response_header_timeout|stream_idle_timeout|request_timeout|transcription_timeout|conspect_max_duration|conspect_idle_timeout)"\s*:\s*(-?\d+)|"(?:[^"\\]|\\.)*"/g,
    (token, digits: string | undefined) => digits === undefined ? token : token.slice(0, token.lastIndexOf(digits)) + JSON.stringify(digits)));
}

// Match positive Go durations, including compound and fractional units.
export function validDuration(value: string): boolean {
  let remaining = value.startsWith('+') ? value.slice(1) : value;
  let total = 0n;
  const scales: Record<string, bigint> = { ns: 1n, us: 1000n, 'µs': 1000n, 'μs': 1000n, ms: 1000000n, s: 1000000000n, m: 60000000000n, h: 3600000000000n };
  if (!remaining) return false;
  while (remaining) {
    const match = /^(\d+(?:\.\d*)?|\.\d+)(ns|us|µs|μs|ms|s|m|h)/.exec(remaining);
    if (!match) return false;
    const [whole, fraction = ''] = match[1].split('.');
    const denominator = 10n ** BigInt(fraction.length);
    total += BigInt((whole || '0') + fraction) * scales[match[2]] / denominator;
    if (total > 9223372036854775807n) return false;
    remaining = remaining.slice(match[0].length);
  }
  return total > 0n;
}
