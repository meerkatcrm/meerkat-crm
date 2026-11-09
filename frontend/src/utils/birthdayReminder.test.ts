import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest';
import { Reminder, ReminderFormData } from '../api/reminders';
import {
  buildBirthdayReminderPayload,
  buildBirthdayReminderUpdate,
  computeNextBirthdayOccurrence,
  findBirthdayReminder
} from './birthdayReminder';

// What the backend does with these requests: PUT validates a whole reminder, and
// both POST and PUT keep only the UTC date (controllers/reminder_controller.go).
class FakeBackend {
  reminders: Reminder[] = [];
  private nextID = 1;

  private storedDate(remindAt: string): string {
    return `${new Date(remindAt).toISOString().slice(0, 10)}T00:00:00Z`;
  }

  create(data: ReminderFormData) {
    this.reminders.push({
      ...data,
      ID: this.nextID++,
      remind_at: this.storedDate(data.remind_at),
      completed: false,
      email_sent: false
    });
  }

  update(id: number, data: Partial<ReminderFormData>) {
    if (!data.message || !data.recurrence || !data.contact_id || !data.remind_at) {
      throw new Error('400 VALIDATION_ERROR');
    }
    const index = this.reminders.findIndex((r) => r.ID === id);
    this.reminders[index] = {
      ...this.reminders[index],
      ...data,
      remind_at: this.storedDate(data.remind_at)
    };
  }

  delete(id: number) {
    this.reminders = this.reminders.filter((r) => r.ID !== id);
  }

  // handleToggleBirthdayReminder in ContactDetailPage
  toggle(birthday: string) {
    const existing = findBirthdayReminder(this.reminders, birthday);
    if (existing) {
      this.delete(existing.ID);
      return;
    }
    this.create(buildBirthdayReminderPayload(1, birthday, 'Birthday')!);
  }

  // The birthday branch of handleSave in ContactDetailPage
  changeBirthday(previous: string, next: string) {
    const existing = findBirthdayReminder(this.reminders, previous);
    if (!existing) return;
    if (!next) {
      this.delete(existing.ID);
      return;
    }
    this.update(existing.ID, buildBirthdayReminderUpdate(existing, next)!);
  }
}

function storedRow(remindAt: string): Reminder {
  return {
    ID: 99,
    message: 'Birthday',
    by_mail: true,
    remind_at: remindAt,
    recurrence: 'yearly',
    reoccur_from_completion: false,
    completed: false,
    email_sent: false,
    contact_id: 1
  };
}

const setLocalTime = (year: number, month: number, day: number, hour = 12, minute = 0) => {
  vi.setSystemTime(new Date(year, month - 1, day, hour, minute));
};

describe.each([
  'Europe/Berlin',
  'Europe/London',
  'Asia/Tokyo',
  'America/New_York',
  'America/Los_Angeles',
  'Australia/Adelaide',
  'Australia/Sydney',
  'Pacific/Auckland',
  'Pacific/Kiritimati', // UTC+14
  'Pacific/Pago_Pago' // UTC-11
])('birthday reminders in %s', (zone) => {
  beforeAll(() => {
    vi.stubEnv('TZ', zone);
    vi.useFakeTimers();
  });

  afterAll(() => {
    vi.useRealTimers();
    vi.unstubAllEnvs();
  });

  it('really runs in that timezone', () => {
    expect(Intl.DateTimeFormat().resolvedOptions().timeZone).toBe(zone);
  });

  describe('create', () => {
    it.each([
      [0, 30],
      [9, 0],
      [23, 30]
    ])('stores the birthday itself when created at %i:%i local time', (hour, minute) => {
      setLocalTime(2026, 11, 9, hour, minute);
      const backend = new FakeBackend();

      backend.create(buildBirthdayReminderPayload(1, '1990-03-15', 'Birthday')!);

      expect(backend.reminders[0].remind_at).toBe('2027-03-15T00:00:00Z');
    });

    it('sends midnight UTC, so the backend has no time of day to discard', () => {
      setLocalTime(2026, 11, 9);
      expect(buildBirthdayReminderPayload(1, '--03-15', 'Birthday')?.remind_at).toBe(
        '2027-03-15T00:00:00.000Z'
      );
    });

    it('schedules this year while the birthday is still ahead', () => {
      setLocalTime(2027, 3, 14, 23, 30);
      expect(computeNextBirthdayOccurrence('1990-03-15')?.toISOString()).toBe('2027-03-15T00:00:00.000Z');
    });

    it('schedules next year on the birthday itself', () => {
      setLocalTime(2027, 3, 15, 0, 30);
      expect(computeNextBirthdayOccurrence('1990-03-15')?.toISOString()).toBe('2028-03-15T00:00:00.000Z');
      setLocalTime(2027, 3, 15, 23, 30);
      expect(computeNextBirthdayOccurrence('1990-03-15')?.toISOString()).toBe('2028-03-15T00:00:00.000Z');
    });

    it('decides by the local date when UTC is on a different day', () => {
      setLocalTime(2026, 12, 31, 23, 30);
      expect(computeNextBirthdayOccurrence('1990-01-01')?.toISOString()).toBe('2027-01-01T00:00:00.000Z');
      setLocalTime(2027, 1, 1, 0, 30);
      expect(computeNextBirthdayOccurrence('1990-01-01')?.toISOString()).toBe('2028-01-01T00:00:00.000Z');
    });

    it('returns nothing for an unparseable birthday', () => {
      expect(computeNextBirthdayOccurrence('')).toBeUndefined();
      expect(computeNextBirthdayOccurrence('not-a-date')).toBeUndefined();
    });
  });

  describe('find', () => {
    it.each(['1990-03-15', '--03-15'])('finds the reminder it created for %s', (birthday) => {
      setLocalTime(2026, 11, 9);
      const backend = new FakeBackend();

      backend.toggle(birthday);

      expect(findBirthdayReminder(backend.reminders, birthday)?.ID).toBe(1);
    });

    it('finds a row already stored at midnight UTC', () => {
      expect(findBirthdayReminder([storedRow('2027-03-15T00:00:00Z')], '1990-03-15')?.ID).toBe(99);
    });

    it('ignores another date, and reminders that are not calendar-pinned yearly ones', () => {
      expect(findBirthdayReminder([storedRow('2027-03-16T00:00:00Z')], '1990-03-15')).toBeUndefined();
      expect(
        findBirthdayReminder(
          [{ ...storedRow('2027-03-15T00:00:00Z'), reoccur_from_completion: true }],
          '1990-03-15'
        )
      ).toBeUndefined();
      expect(
        findBirthdayReminder([{ ...storedRow('2027-03-15T00:00:00Z'), recurrence: 'once' }], '1990-03-15')
      ).toBeUndefined();
    });

    it('does not claim a row stored a day early', () => {
      expect(findBirthdayReminder([storedRow('2027-03-14T00:00:00Z')], '1990-03-15')).toBeUndefined();
    });
  });

  describe('toggle off', () => {
    it('removes the reminder rather than creating a duplicate', () => {
      setLocalTime(2026, 11, 9);
      const backend = new FakeBackend();

      backend.toggle('1990-03-15');
      expect(backend.reminders).toHaveLength(1);

      backend.toggle('1990-03-15');
      expect(backend.reminders).toHaveLength(0);
      expect(findBirthdayReminder(backend.reminders, '1990-03-15')).toBeUndefined();
    });
  });

  describe('birthday change', () => {
    it('moves the reminder and keeps everything else', () => {
      setLocalTime(2026, 11, 9);
      const backend = new FakeBackend();
      backend.toggle('1990-03-15');

      backend.changeBirthday('1990-03-15', '1990-04-20');

      expect(backend.reminders).toHaveLength(1);
      expect(backend.reminders[0]).toMatchObject({
        message: 'Birthday',
        by_mail: true,
        recurrence: 'yearly',
        reoccur_from_completion: false,
        contact_id: 1,
        remind_at: '2027-04-20T00:00:00Z'
      });
      expect(findBirthdayReminder(backend.reminders, '1990-03-15')).toBeUndefined();
      expect(findBirthdayReminder(backend.reminders, '1990-04-20')?.ID).toBe(1);
    });

    it('removes the reminder when the birthday is cleared', () => {
      setLocalTime(2026, 11, 9);
      const backend = new FakeBackend();
      backend.toggle('1990-03-15');

      backend.changeBirthday('1990-03-15', '');

      expect(backend.reminders).toHaveLength(0);
    });
  });

  describe('Feb 29 birthdays', () => {
    it('falls on Feb 28 in a non-leap year and Feb 29 in a leap year', () => {
      setLocalTime(2026, 11, 9);
      expect(computeNextBirthdayOccurrence('1996-02-29')?.toISOString()).toBe('2027-02-28T00:00:00.000Z');
      setLocalTime(2027, 11, 9);
      expect(computeNextBirthdayOccurrence('1996-02-29')?.toISOString()).toBe('2028-02-29T00:00:00.000Z');
    });

    it('moves on to the next leap year once Feb 28 has come', () => {
      setLocalTime(2027, 2, 28);
      expect(computeNextBirthdayOccurrence('1996-02-29')?.toISOString()).toBe('2028-02-29T00:00:00.000Z');
    });

    it.each([
      ['a leap year', '2028-02-29T00:00:00Z'],
      ['a non-leap year', '2027-02-28T00:00:00Z'],
      ['a row from before the clamp', '2027-03-01T00:00:00Z']
    ])('finds the reminder on %s', (_name, remindAt) => {
      expect(findBirthdayReminder([storedRow(remindAt)], '1996-02-29')?.ID).toBe(99);
    });

    it('toggles on and off in a non-leap year', () => {
      setLocalTime(2026, 11, 9);
      const backend = new FakeBackend();

      backend.toggle('1996-02-29');
      expect(backend.reminders[0].remind_at).toBe('2027-02-28T00:00:00Z');

      backend.toggle('1996-02-29');
      expect(backend.reminders).toHaveLength(0);
    });

    it('does not widen the match for other birthdays', () => {
      expect(findBirthdayReminder([storedRow('2027-03-01T00:00:00Z')], '1996-02-28')).toBeUndefined();
      expect(findBirthdayReminder([storedRow('2028-02-29T00:00:00Z')], '1996-03-01')).toBeUndefined();
    });
  });
});
