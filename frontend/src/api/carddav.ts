import { apiJson, ApiError } from './client';

export type CardDAVDirection = 'two_way' | 'pull' | 'push';

export interface CardDAVConnection {
  id: number;
  base_url: string;
  address_book_path: string;
  address_book_name: string;
  username: string;
  has_password: boolean;
  direction: CardDAVDirection;
  sync_enabled: boolean;
  /** True while a sync started via startCardDAVSync is still running. */
  sync_running: boolean;
  last_synced_at: string | null;
  last_sync_status: '' | 'success' | 'partial' | 'error';
  last_sync_error: string;
  /** Counts from the last completed run, or null before the first one. */
  last_sync_stats: ContactSyncStats | null;
  created_at: string;
}

export interface CardDAVConnectionInput {
  base_url: string;
  address_book_path: string;
  address_book_name?: string;
  username?: string;
  password?: string;
  clear_password?: boolean;
  direction?: CardDAVDirection;
  sync_enabled?: boolean;
}

export interface DiscoveredAddressBook {
  path: string;
  name: string;
  description: string;
}

export interface ContactSyncStats {
  pulled_created: number;
  pulled_updated: number;
  pulled_deleted: number;
  pushed_created: number;
  pushed_updated: number;
  pushed_deleted: number;
  conflicts: number;
  skipped: number;
  errors: number;
}


// Returns null when no connection is configured yet (404).
export async function getCardDAVConnection(): Promise<CardDAVConnection | null> {
  try {
    return await apiJson<CardDAVConnection>('/carddav/connection', {
      errorMessage: 'Unable to load the CardDAV connection.',
    });
  } catch (err) {
    if (err instanceof ApiError && err.status === 404) {
      return null;
    }
    throw err;
  }
}

export async function saveCardDAVConnection(
  input: CardDAVConnectionInput
): Promise<CardDAVConnection> {
  return apiJson<CardDAVConnection>('/carddav/connection', {
    method: 'PUT',
    body: JSON.stringify(input),
    errorMessage: 'Unable to save the CardDAV connection.',
  });
}

export async function deleteCardDAVConnection(): Promise<void> {
  await apiJson('/carddav/connection', {
      method: 'DELETE',
      errorMessage: 'Unable to disconnect the CardDAV server.',
    });
  }

  export async function discoverCardDAVAddressBooks(input: {
    base_url: string;
    username?: string;
    password?: string;
  }): Promise<DiscoveredAddressBook[]> {
    const data = await apiJson<{ address_books?: DiscoveredAddressBook[] } | undefined>('/carddav/discover', {
      method: 'POST',
      body: JSON.stringify(input),
      timeout: 60000,
      errorMessage: 'Unable to discover address books.',
    });
    return data?.address_books || [];
  }

  /**
   * Starts a sync on the server and returns as soon as it has been accepted (202).
   * A first sync can run for minutes, so the outcome is not awaited here — poll
   * getCardDAVConnection and watch `sync_running`, then read `last_sync_status`
   * and `last_sync_stats`.
   */
  export async function startCardDAVSync(): Promise<void> {
    await apiJson('/carddav/connection/sync', {
      method: 'POST',
      errorMessage: 'Unable to start the contact sync.',
    });
  }
