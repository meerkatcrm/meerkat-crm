// Contact-related API calls
import { apiFetch, apiJson, API_BASE_URL, getAuthHeaders, parseErrorResponse } from './client';

export interface ContactValue {
  type: string;
  value: string;
}

export interface ContactAddress {
  type: string;
  street: string;
  city: string;
  region: string;
  postal: string;
  country: string;
}

export interface Contact {
  ID: number;
  firstname: string;
  lastname: string;
  nickname?: string;
  gender?: string;
  email?: string;
  phone?: string;
  birthday?: string;
  photo?: string;
  address?: string;
  how_we_met?: string;
  food_preference?: string;
  work_information?: string;
  contact_information?: string;
  note?: string;
  circles?: string[];
  photo_thumbnail?: string;
  custom_fields?: Record<string, string>;
  archived?: boolean;
  // Multi-valued vCard fields
  emails?: ContactValue[];
  phones?: ContactValue[];
  addresses?: ContactAddress[];
  urls?: ContactValue[];
  impps?: ContactValue[];
  // Structured name parts
  prefix?: string;
  middle_name?: string;
  suffix?: string;
  // Organizational fields
  organization?: string;
  department?: string;
  job_title?: string;
  role?: string;
  anniversary?: string;
  is_deceased?: boolean;
  deceased_date?: string;
}

export interface Birthday {
  type: 'contact' | 'relationship';
  name: string;
  birthday: string;
  photo_thumbnail?: string;
  contact_id: number;
  relationship_type?: string;
  associated_contact_name?: string;
}

export interface ContactsResponse {
  contacts: Contact[];
  total: number;
  page: number;
  limit: number;
}

export interface GetContactsParams {
  page?: number;
  limit?: number;
  search?: string;
  circle?: string;
  sort?: string;
  order?: string;
  includeArchived?: boolean;
  archived?: boolean;
}

// Get all contacts with pagination and filters
export async function getContacts(
  params: GetContactsParams
): Promise<ContactsResponse> {
  const { page = 1, limit = 25, search = '', circle = '', sort, order, includeArchived, archived } = params;

  const queryParams = new URLSearchParams({
    page: page.toString(),
    limit: limit.toString(),
  });

  if (search) queryParams.append('search', search);
  if (circle) queryParams.append('circle', circle);
  if (sort) queryParams.append('sort', sort);
  if (order) queryParams.append('order', order);
  if (includeArchived) queryParams.append('include_archived', 'true');
  if (archived !== undefined) queryParams.append('archived', archived.toString());

  queryParams.append('fields', 'ID,firstname,lastname,nickname,circles,photo_thumbnail,archived');

  return apiJson(`/contacts?${queryParams.toString()}`);
}

// Get single contact
export async function getContact(
  id: string | number,
  fields?: string[]
): Promise<Contact> {
  let url = `${API_BASE_URL}/contacts/${id}`;
  if (fields && fields.length > 0) {
    url += `?fields=${fields.join(',')}`;
  }

  const response = await apiFetch(
    url,
    { headers: getAuthHeaders() }
  );

  if (!response.ok) {
    throw await parseErrorResponse(response);
  }

  return response.json();
}

// Create contact
export async function createContact(
  data: Partial<Contact>
): Promise<Contact> {
  const response = await apiFetch(
    `${API_BASE_URL}/contacts`,
    {
      method: 'POST',
      headers: getAuthHeaders(),
      body: JSON.stringify(data),
    }
  );

  if (!response.ok) {
    throw await parseErrorResponse(response);
  }

  const result = await response.json();
  return result.contact || result;
}

// Update contact.
// Callers pass the whole contact, so the employment fields are removed here: the UI
// changes them only through the employment history endpoints, and a stale copy in
// another tab must not overwrite a newer job.
export async function updateContact(
  id: string | number,
  data: Partial<Contact>
): Promise<Contact> {
  const { organization, department, job_title, role, ...payload } = data;
  return apiJson(`/contacts/${id}`, {
    method: 'PUT',
    body: JSON.stringify(payload),
  });
}

// Delete contact
export async function deleteContact(
  id: string | number
): Promise<void> {
  await apiJson(`/contacts/${id}`, {
    method: 'DELETE',
  });
}

// Get contact profile picture
export async function getContactProfilePicture(
  id: string | number,
  thumbnail: boolean = false
): Promise<Blob | null> {
  const url = thumbnail
    ? `${API_BASE_URL}/contacts/${id}/profile_picture?thumbnail=true`
    : `${API_BASE_URL}/contacts/${id}/profile_picture`;
  const response = await apiFetch(url);

  if (!response.ok) {
    return null;
  }

  return response.blob();
}

// Upload contact profile picture
export async function uploadProfilePicture(
  id: string | number,
  imageBlob: Blob
): Promise<void> {
  const formData = new FormData();
  formData.append('photo', imageBlob, 'profile.jpg');

  await apiJson(`/contacts/${id}/profile_picture`, {
    method: 'POST',
    body: formData,
  });
}

// Get all circles
export async function getCircles(): Promise<string[]> {
  const data = await apiJson<unknown>('/contacts/circles');
  // Backend returns array directly, not wrapped in object
  return Array.isArray(data) ? data : [];
}

// Get random contacts (returns 5 contacts)
export async function getRandomContacts(): Promise<Contact[]> {
  const data = await apiJson<{ contacts?: Contact[] }>('/contacts/random');
  return data.contacts || [];
}

// Get upcoming birthdays (returns up to 10 birthdays for contacts and relationships)
export async function getUpcomingBirthdays(): Promise<Birthday[]> {
  const data = await apiJson<{ birthdays?: Birthday[] }>('/contacts/birthdays');
  return data.birthdays || [];
}

// Archive a contact (deletes all reminders)
export async function archiveContact(
  id: string | number
): Promise<Contact> {
  return apiJson(`/contacts/${id}/archive`, {
    method: 'POST',
  });
}

// Unarchive a contact
export async function unarchiveContact(
  id: string | number
): Promise<Contact> {
  return apiJson(`/contacts/${id}/unarchive`, {
    method: 'POST',
  });
}

export type DuplicateReason = 'name' | 'email' | 'phone';

export interface DuplicateContactSummary {
  ID: number;
  firstname: string;
  lastname: string;
  nickname: string;
  email: string;
  phone: string;
  organization: string;
  photo_thumbnail: string;
  archived: boolean;
}

export interface DuplicatePair {
  contacts: [DuplicateContactSummary, DuplicateContactSummary];
  reasons: DuplicateReason[];
}

// A field both contacts fill with different values. `field` is the contact's
// JSON field name, "photo", or "custom_fields.<name>".
export interface MergeConflict {
  field: string;
  target_value: string;
  source_value: string;
}

export interface MergePreview {
  target: Contact;
  source: Contact;
  conflicts: MergeConflict[];
  moved: {
    notes: number;
    activities: number;
    reminders: number;
    relationships: number;
    employment_histories: number;
  };
}

export type MergeResolution = 'target' | 'source';

// List pairs of contacts that probably describe the same person
export async function getDuplicateContacts(): Promise<DuplicatePair[]> {
  const response = await apiFetch(
    `${API_BASE_URL}/contacts/duplicates`,
    { headers: getAuthHeaders() }
  );

  if (!response.ok) {
    throw await parseErrorResponse(response);
  }

  const result = await response.json();
  return result.duplicates || [];
}

// Mark two contacts as not duplicates of each other
export async function dismissDuplicate(
  contactAId: number,
  contactBId: number
): Promise<void> {
  await apiJson('/contacts/duplicates/dismiss', {
    method: 'POST',
    body: JSON.stringify({ contact_a_id: contactAId, contact_b_id: contactBId }),
  });
}

// Preview merging the source contact into the target
export async function getMergePreview(
  targetId: string | number,
  sourceId: string | number
): Promise<MergePreview> {
  return apiJson(`/contacts/${targetId}/merge-preview?source_id=${encodeURIComponent(sourceId)}`);
}

// Merge the source contact into the target; the source is deleted
export async function mergeContacts(
  targetId: string | number,
  sourceId: number,
  resolutions: Record<string, MergeResolution>
): Promise<Contact> {
  return apiJson(`/contacts/${targetId}/merge`, {
    method: 'POST',
    body: JSON.stringify({ source_id: sourceId, resolutions }),
  });
}
