// User settings and preferences API calls
import { apiJson } from './client';

export async function updateLanguage(language: string): Promise<string> {
  const data = await apiJson<{ message?: string } | undefined>('/users/language', {
    method: 'PATCH',
    body: JSON.stringify({ language }),
    errorMessage: 'Unable to update language.',
  });
  return data?.message || 'Language updated successfully.';
}

export async function updateDateFormat(dateFormat: string): Promise<string> {
  const data = await apiJson<{ message?: string } | undefined>('/users/date-format', {
    method: 'PATCH',
    body: JSON.stringify({ date_format: dateFormat }),
    errorMessage: 'Unable to update date format.',
  });
  return data?.message || 'Date format updated successfully.';
}

export async function getCustomFieldNames(): Promise<string[]> {
  const data = await apiJson<{ custom_field_names?: string[] } | undefined>('/users/custom-fields', {
    errorMessage: 'Unable to get custom field names.',
  });
  return data?.custom_field_names || [];
}

export async function updateCustomFieldNames(names: string[]): Promise<string[]> {
  const data = await apiJson<{ custom_field_names?: string[] } | undefined>('/users/custom-fields', {
    method: 'PATCH',
    body: JSON.stringify({ names }),
    errorMessage: 'Unable to update custom field names.',
  });
  return data?.custom_field_names || names;
}

export type CardDAVCircleMode = 'all' | 'include' | 'exclude';

export interface CardDAVCircleFilter {
  mode: CardDAVCircleMode;
  circles: string[];
}

// Which circles Meerkat's built-in CardDAV server shows the user's devices.
export async function getCardDAVCircleFilter(): Promise<CardDAVCircleFilter> {
  const data = await apiJson<Partial<CardDAVCircleFilter> | undefined>('/users/carddav-circle-filter', {
    errorMessage: 'Unable to get the CardDAV circle filter.',
  });
  return { mode: data?.mode ?? 'all', circles: data?.circles ?? [] };
}

export async function updateCardDAVCircleFilter(filter: CardDAVCircleFilter): Promise<CardDAVCircleFilter> {
  const data = await apiJson<Partial<CardDAVCircleFilter> | undefined>('/users/carddav-circle-filter', {
    method: 'PATCH',
    body: JSON.stringify(filter),
    errorMessage: 'Unable to update the CardDAV circle filter.',
  });
  return { mode: data?.mode ?? filter.mode, circles: data?.circles ?? filter.circles };
}

export async function updateEnabledContactFields(fields: string[]): Promise<string[]> {
  const data = await apiJson<{ enabled_contact_fields?: string[] } | undefined>('/users/enabled-contact-fields', {
    method: 'PATCH',
    body: JSON.stringify({ fields }),
    errorMessage: 'Unable to update enabled contact fields.',
  });
  return data?.enabled_contact_fields || fields;
}

/**
 * Marks a contact as the user themself ("me"), or clears the mark with null.
 * At most one contact is "me": marking another one moves the mark.
 */
export async function setSelfContact(contactId: number | null): Promise<number | null> {
  if (contactId === null) {
    await apiJson('/users/self-contact', { method: 'DELETE', errorMessage: 'Unable to update which contact is you.' });
    return null;
  }
  await apiJson('/users/self-contact', {
    method: 'PUT',
    body: JSON.stringify({ contact_id: contactId }),
    errorMessage: 'Unable to update which contact is you.',
  });
  return contactId;
}

export async function updateContactFieldDisplay(hideEmpty: boolean): Promise<boolean> {
  const data = await apiJson<{ hide_empty_contact_fields?: boolean } | undefined>('/users/contact-field-display', {
    method: 'PATCH',
    body: JSON.stringify({ hide_empty_contact_fields: hideEmpty }),
    errorMessage: 'Unable to update contact field display.',
  });
  return data?.hide_empty_contact_fields ?? hideEmpty;
}
