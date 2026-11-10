import { apiJson } from './client';

export interface CalendarSubscription {
  id: number;
  name: string;
  url: string;
  username: string;
  has_password: boolean;
  sync_enabled: boolean;
  past_days: number;
  future_days: number;
  last_synced_at: string | null;
  last_sync_status: '' | 'success' | 'error';
  last_sync_error: string;
  created_at: string;
}

export interface CalendarSubscriptionInput {
  name: string;
  url: string;
  username?: string;
  password?: string;
  clear_password?: boolean;
  sync_enabled?: boolean;
  past_days?: number;
  future_days?: number;
}

export interface CalendarSyncResult {
  message: string;
  created: number;
  updated: number;
  skipped: number;
}

export async function getCalendarSubscriptions(): Promise<CalendarSubscription[]> {
  const data = await apiJson<{ calendars?: CalendarSubscription[] } | undefined>('/calendars', {
    errorMessage: 'Unable to load calendar subscriptions.',
  });
  return data?.calendars || [];
}

export async function createCalendarSubscription(
  input: CalendarSubscriptionInput
): Promise<CalendarSubscription> {
  return apiJson<CalendarSubscription>('/calendars', {
    method: 'POST',
    body: JSON.stringify(input),
    errorMessage: 'Unable to add calendar.',
  });
}

export async function updateCalendarSubscription(
  id: number,
  input: CalendarSubscriptionInput
): Promise<CalendarSubscription> {
  return apiJson<CalendarSubscription>(`/calendars/${id}`, {
    method: 'PUT',
    body: JSON.stringify(input),
    errorMessage: 'Unable to update calendar.',
  });
}

export async function deleteCalendarSubscription(id: number): Promise<void> {
  await apiJson(`/calendars/${id}`, {
      method: 'DELETE',
      errorMessage: 'Unable to delete calendar.',
    });
  }

  export async function syncCalendarSubscription(id: number): Promise<CalendarSyncResult> {
    return apiJson<CalendarSyncResult>(`/calendars/${id}/sync`, {
      method: 'POST',
      timeout: 120000,
      errorMessage: 'Unable to sync calendar.',
    });
  }
