import { Reminder, ReminderFormData } from '../api/reminders';
import { now } from './now';

// Extracts month (0-indexed) and day from a stored birthday, which is either
// a full ISO date ("YYYY-MM-DD") or a year-less date ("--MM-DD").
export function parseBirthdayMonthDay(birthdayISO: string): { month: number; day: number } | undefined {
  if (!birthdayISO) return undefined;

  let month: number;
  let day: number;

  if (birthdayISO.startsWith('--')) {
    month = parseInt(birthdayISO.substring(2, 4), 10) - 1;
    day = parseInt(birthdayISO.substring(5, 7), 10);
  } else {
    const parts = birthdayISO.split('-');
    if (parts.length !== 3) return undefined;
    month = parseInt(parts[1], 10) - 1;
    day = parseInt(parts[2], 10);
  }

  if (isNaN(month) || isNaN(day)) return undefined;
  return { month, day };
}

// The birthday's date in the given year as a UTC timestamp. A Feb 29 birthday
// falls on Feb 28 in a non-leap year, which is where the backend's addYears
// puts a yearly reminder too.
function birthdayInYear(year: number, { month, day }: { month: number; day: number }): number {
  const lastDayOfMonth = new Date(Date.UTC(year, month + 1, 0)).getUTCDate();
  return Date.UTC(year, month, Math.min(day, lastDayOfMonth));
}

// A birthday reminder is a calendar date, not an instant: its next occurrence
// is midnight UTC of the birthday, whatever the browser's timezone. The backend
// keeps only the UTC date, and the rest of the app reads it back with UTC getters.
//
// "Today" is the browser's own date. On the birthday itself this schedules next
// year: the daily email already lists today's birthdays, and a reminder set after
// that email went out would only arrive tomorrow.
export function computeNextBirthdayOccurrence(birthdayISO: string): Date | undefined {
  const monthDay = parseBirthdayMonthDay(birthdayISO);
  if (!monthDay) return undefined;

  const current = now();
  const today = Date.UTC(current.getFullYear(), current.getMonth(), current.getDate());
  const thisYear = birthdayInYear(current.getFullYear(), monthDay);

  return new Date(thisYear > today ? thisYear : birthdayInYear(current.getFullYear() + 1, monthDay));
}

export function buildBirthdayReminderPayload(
  contactId: number,
  birthdayISO: string,
  message: string
): ReminderFormData | undefined {
  const nextOccurrence = computeNextBirthdayOccurrence(birthdayISO);
  if (!nextOccurrence) return undefined;

  return {
    message,
    by_mail: true,
    remind_at: nextOccurrence.toISOString(),
    recurrence: 'yearly',
    reoccur_from_completion: false,
    contact_id: contactId
  };
}

// A birthday can change while its reminder stays: PUT /reminders/:id validates a
// whole reminder, so everything but the date is sent back as it is.
export function buildBirthdayReminderUpdate(
  reminder: Reminder,
  birthdayISO: string
): ReminderFormData | undefined {
  const nextOccurrence = computeNextBirthdayOccurrence(birthdayISO);
  if (!nextOccurrence) return undefined;

  return {
    message: reminder.message,
    by_mail: reminder.by_mail,
    remind_at: nextOccurrence.toISOString(),
    recurrence: reminder.recurrence,
    reoccur_from_completion: reminder.reoccur_from_completion,
    contact_id: reminder.contact_id
  };
}

function fallsOnBirthday(remindAt: Date, { month, day }: { month: number; day: number }): boolean {
  const reminderMonth = remindAt.getUTCMonth();
  const reminderDay = remindAt.getUTCDate();
  if (reminderMonth === month && reminderDay === day) return true;

  // Feb 29 has no date of its own in most years: yearly reminders sit on Feb 28,
  // and ones created before the clamp in birthdayInYear rolled over to Mar 1.
  if (month !== 1 || day !== 29) return false;
  return (reminderMonth === 1 && reminderDay === 28) || (reminderMonth === 2 && reminderDay === 1);
}

// Heuristic match: a birthday reminder is a yearly, calendar-pinned reminder
// whose UTC date matches the contact's birthday. There is no dedicated "type"
// field on Reminder, so this convention is relied on across the app (see
// backend/services/monica_import_service.go's isBirthdayReminder).
export function findBirthdayReminder(reminders: Reminder[], birthdayISO: string): Reminder | undefined {
  const monthDay = parseBirthdayMonthDay(birthdayISO);
  if (!monthDay) return undefined;

  const matches = reminders.filter((reminder) => {
    if (reminder.recurrence !== 'yearly' || reminder.reoccur_from_completion) return false;
    return fallsOnBirthday(new Date(reminder.remind_at), monthDay);
  });

  if (matches.length === 0) return undefined;

  return matches.reduce((earliest, current) =>
    new Date(current.remind_at) < new Date(earliest.remind_at) ? current : earliest
  );
}
