import { test, expect, vi, afterEach } from 'vitest';
import { isReminderOverdue } from './reminderDue';

afterEach(() => {
  vi.useRealTimers();
});

// Local noon, so the local calendar date is the one named here in any timezone.
function setToday(year: number, month: number, day: number, hour = 12) {
  vi.useFakeTimers({ toFake: ['Date'] });
  vi.setSystemTime(new Date(year, month - 1, day, hour));
}

test('a reminder due today is not overdue, at any time of day', () => {
  setToday(2026, 6, 15, 0);
  expect(isReminderOverdue('2026-06-15T00:00:00Z')).toBe(false);
  setToday(2026, 6, 15, 23);
  expect(isReminderOverdue('2026-06-15T00:00:00Z')).toBe(false);
});

test('a reminder due yesterday is overdue, even just after local midnight', () => {
  setToday(2026, 6, 15, 0);
  expect(isReminderOverdue('2026-06-14T00:00:00Z')).toBe(true);
});

test('a reminder due tomorrow is not overdue', () => {
  setToday(2026, 6, 15, 23);
  expect(isReminderOverdue('2026-06-16T00:00:00Z')).toBe(false);
});

test('month and year boundaries compare by calendar date', () => {
  setToday(2027, 1, 1);
  expect(isReminderOverdue('2026-12-31T00:00:00Z')).toBe(true);
  expect(isReminderOverdue('2027-01-01T00:00:00Z')).toBe(false);
});

test('a missing date is not overdue', () => {
  expect(isReminderOverdue('')).toBe(false);
});
