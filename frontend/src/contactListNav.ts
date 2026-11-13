import type { GetContactsParams } from './api/contacts';

// Where a contact sits in the list it was opened from, carried in the router's
// location state so the detail page can step to the previous or next one.
export interface ContactListNav {
  // The list's filters and order, without page and limit
  params: Omit<GetContactsParams, 'page' | 'limit'>;
  // 0-based position in the whole filtered list
  index: number;
  total: number;
}

export function readContactListNav(state: unknown): ContactListNav | null {
  const nav = (state as { listNav?: ContactListNav } | null)?.listNav;
  if (!nav || typeof nav.index !== 'number' || typeof nav.total !== 'number' || !nav.params) {
    return null;
  }
  return nav;
}

// A random order is drawn anew on every request, so it has no next contact.
export function isNavigableSort(sort: string | undefined): boolean {
  return sort !== 'random';
}
