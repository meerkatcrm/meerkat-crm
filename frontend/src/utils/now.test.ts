import { afterEach, describe, expect, it, vi } from 'vitest';
import { now } from './now';

describe('now', () => {
  afterEach(() => vi.useRealTimers());

  it('reads the current time on every call', () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2031-02-03T04:05:06Z'));
    expect(now().toISOString()).toBe('2031-02-03T04:05:06.000Z');
    vi.setSystemTime(new Date('2031-02-04T00:00:00Z'));
    expect(now().toISOString()).toBe('2031-02-04T00:00:00.000Z');
  });

  it('returns a new Date each time, so callers may change it', () => {
    expect(now()).not.toBe(now());
  });
});
