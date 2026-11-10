import { todayISO } from './today';

// remind_at is stored as midnight UTC of a calendar day, so only its date part
// means anything. A reminder is overdue once that day lies before today's date
// where the user is; one due today is not overdue yet.
export function isReminderOverdue(remindAt: string): boolean {
  if (!remindAt) return false;
  return remindAt.slice(0, 10) < todayISO();
}
