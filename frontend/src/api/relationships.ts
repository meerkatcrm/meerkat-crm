// Relationship-related API calls
import { apiFetch, apiJson, API_BASE_URL, getAuthHeaders, parseErrorResponse } from './client';
import { Contact } from './contacts';

export interface Relationship {
  ID: number;
  CreatedAt: string;
  UpdatedAt: string;
  name: string;
  type: string;
  gender?: string;
  birthday?: string;
  start_date?: string;
  end_date?: string;
  contact_id: number;
  related_contact_id?: number;
  related_contact?: Pick<Contact, 'ID' | 'firstname' | 'lastname' | 'gender' | 'birthday'>;
}

export interface RelationshipFormData {
  name: string;
  type: string;
  gender?: string;
  birthday?: string;
  start_date?: string;
  end_date?: string;
  related_contact_id?: number | null;
}

export interface RelationshipsResponse {
  relationships: Relationship[];
}

export interface IncomingRelationship {
  ID: number;
  CreatedAt: string;
  UpdatedAt: string;
  name: string;
  type: string;
  gender?: string;
  birthday?: string;
  start_date?: string;
  end_date?: string;
  contact_id: number;
  source_contact: Pick<Contact, 'ID' | 'firstname' | 'lastname'>;
}

export interface IncomingRelationshipsResponse {
  incoming_relationships: IncomingRelationship[];
}

// Whether a relationship has ended (it carries an end date)
export function isRelationshipEnded(rel: { end_date?: string }): boolean {
  return !!rel.end_date;
}

// Pads a partial date (YYYY, YYYY-MM, YYYY-MM-DD) to the first or last day of its
// period, so two partial dates compare correctly as plain strings.
function padPartialDate(value: string, edge: 'start' | 'end'): string {
  if (/^\d{4}$/.test(value)) return edge === 'start' ? `${value}-01-01` : `${value}-12-31`;
  if (/^\d{4}-\d{2}$/.test(value)) return edge === 'start' ? `${value}-01` : `${value}-31`;
  return value;
}

// Mirrors the backend rule: an end date (taken at the end of its period) must not be
// before the start date (taken at the start of its period). Either may be empty.
export function isEndBeforeStart(startDate?: string, endDate?: string): boolean {
  if (!startDate || !endDate) return false;
  return padPartialDate(endDate, 'end') < padPartialDate(startDate, 'start');
}

// Whether a partial date has not begun yet. The period counts from its first day,
// so "2026" is not in the future at any point during 2026. Used to mirror the
// backend rule that an end date must not lie in the future, and to word a start
// date that is still ahead ("starting …" rather than "since …").
export function isPartialDateInFuture(date?: string, today: Date = new Date()): boolean {
  if (!date) return false;
  const pad = (n: number) => String(n).padStart(2, '0');
  const todayISO = `${today.getFullYear()}-${pad(today.getMonth() + 1)}-${pad(today.getDate())}`;
  return padPartialDate(date, 'start') > todayISO;
}

// Get all relationships for a contact
export async function getRelationships(
  contactId: number | string
): Promise<RelationshipsResponse> {
  return apiJson(`/contacts/${contactId}/relationships`);
}

// Get all incoming relationships for a contact (relationships pointing to this contact)
export async function getIncomingRelationships(
  contactId: number | string
): Promise<IncomingRelationshipsResponse> {
  return apiJson(`/contacts/${contactId}/incoming-relationships`);
}

// Create a new relationship
export async function createRelationship(
  contactId: number | string,
  data: RelationshipFormData
): Promise<Relationship> {
  const response = await apiFetch(
    `${API_BASE_URL}/contacts/${contactId}/relationships`,
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
  return result.relationship || result;
}

// Update an existing relationship
export async function updateRelationship(
  contactId: number | string,
  relationshipId: number,
  data: RelationshipFormData
): Promise<Relationship> {
  const response = await apiFetch(
    `${API_BASE_URL}/contacts/${contactId}/relationships/${relationshipId}`,
    {
      method: 'PUT',
      headers: getAuthHeaders(),
      body: JSON.stringify(data),
    }
  );

  if (!response.ok) {
    throw await parseErrorResponse(response);
  }

  const result = await response.json();
  return result.relationship || result;
}

// Delete a relationship
export async function deleteRelationship(
  contactId: number | string,
  relationshipId: number
): Promise<void> {
  await apiJson(`/contacts/${contactId}/relationships/${relationshipId}`, {
    method: 'DELETE',
  });
}

// Preset relationship types
export const RELATIONSHIP_TYPES = [
  'Child',
  'Parent',
  'Mother',
  'Father',
  'Spouse',
  'Partner',
  'Sibling',
  'Brother',
  'Sister',
  'Friend',
  'Best Friend',
  'Colleague',
  'Manager',
  'Employee',
  'Neighbor',
  'Other',
] as const;
