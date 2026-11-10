// Reminder-related API calls
import { apiJson } from './client';

export interface Reminder {
  ID: number;
  message: string;
  by_mail: boolean;
  remind_at: string; // ISO date string
  recurrence: 'once' | 'weekly' | 'monthly' | 'quarterly' | 'six-months' | 'yearly';
  reoccur_from_completion: boolean;
  completed: boolean;
  email_sent: boolean;
  last_sent?: string; // ISO date string
  contact_id: number;
  CreatedAt?: string;
  UpdatedAt?: string;
  DeletedAt?: string | null;
}

export interface ReminderFormData {
  message: string;
  by_mail: boolean;
  remind_at: string; // ISO date string
  recurrence: 'once' | 'weekly' | 'monthly' | 'quarterly' | 'six-months' | 'yearly';
  reoccur_from_completion: boolean;
  contact_id: number;
}

export interface RemindersResponse {
  reminders: Reminder[];
}

export interface ReminderCompletion {
  ID: number;
  reminder_id?: number;
  contact_id: number;
  message: string;
  completed_at: string;
  CreatedAt?: string;
  UpdatedAt?: string;
}

export interface CompletionsResponse {
  completions: ReminderCompletion[];
}

// Get upcoming reminders (next 7 days or at least next 10 reminders)
export async function getUpcomingReminders(): Promise<Reminder[]> {
  const data = await apiJson<RemindersResponse>('/reminders/upcoming');
  return data.reminders || [];
}

// Get reminders for a specific contact
export async function getRemindersForContact(contactId: number): Promise<Reminder[]> {
  const data = await apiJson<RemindersResponse>(`/contacts/${contactId}/reminders`);
  return data.reminders || [];
}

// Create a new reminder
export async function createReminder(
  contactId: number,
  reminderData: ReminderFormData
): Promise<Reminder> {
  const data = await apiJson<{ reminder: Reminder }>(`/contacts/${contactId}/reminders`, {
    method: 'POST',
    body: JSON.stringify(reminderData),
  });
  return data.reminder;
}

// Update an existing reminder
export async function updateReminder(
  reminderId: number,
  reminderData: Partial<ReminderFormData>
): Promise<Reminder> {
  const data = await apiJson<{ reminder: Reminder }>(`/reminders/${reminderId}`, {
    method: 'PUT',
    body: JSON.stringify(reminderData),
  });
  return data.reminder;
}

// Complete a reminder (marks as done and reschedules if recurring)
export async function completeReminder(
  reminderId: number
): Promise<{ message: string; reminder?: Reminder }> {
  return apiJson(`/reminders/${reminderId}/complete`, {
    method: 'POST',
  });
}

// Skip a reminder (reschedules recurring reminders without recording completion)
export async function skipReminder(
  reminderId: number
): Promise<{ message: string; reminder?: Reminder }> {
  return apiJson(`/reminders/${reminderId}/complete?skip=true`, {
    method: 'POST',
  });
}

// Delete a reminder
export async function deleteReminder(reminderId: number): Promise<void> {
  await apiJson(`/reminders/${reminderId}`, {
    method: 'DELETE',
  });
}

// Get reminder completions for a specific contact
export async function getCompletionsForContact(contactId: number): Promise<ReminderCompletion[]> {
  const data = await apiJson<CompletionsResponse>(`/contacts/${contactId}/reminder-completions`);
  return data.completions || [];
}

// Delete a reminder completion
export async function deleteCompletion(completionId: number): Promise<void> {
  await apiJson(`/reminder-completions/${completionId}`, {
    method: 'DELETE',
  });
}
