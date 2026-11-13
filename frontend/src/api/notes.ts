// Notes-related API calls. The types come from the backend's OpenAPI document.
import { apiJson } from './client';
import type { components } from '../types/generated/api';

type Schemas = components['schemas'];
export type Note = Schemas['Note'];
export type NoteBody = Schemas['NoteBody'];
export type ContactNotes = Schemas['ContactNotes'];
export type NotePage = Schemas['NotePage'];
type NoteSaved = Schemas['NoteSaved'];

export interface GetNotesParams {
  page?: number;
  limit?: number;
  search?: string;
  fromDate?: string;
  toDate?: string;
}

// Get notes for a contact
export async function getContactNotes(
  contactId: string | number
): Promise<ContactNotes> {
  return apiJson(`/contacts/${contactId}/notes`);
}

// Get all unassigned notes
export async function getUnassignedNotes(
  params: GetNotesParams = {}
): Promise<NotePage> {
  const { page = 1, limit = 25 } = params;
  const search = params.search?.trim();

  const queryParams = new URLSearchParams({
    page: page.toString(),
    limit: limit.toString(),
  });

  if (search) {
    queryParams.append('search', search);
  }

  if (params.fromDate) {
    queryParams.append('fromDate', params.fromDate);
  }

  if (params.toDate) {
    queryParams.append('toDate', params.toDate);
  }

  return apiJson(`/notes?${queryParams.toString()}`);
}

// Create note for contact
export async function createNote(
  contactId: string | number,
  data: NoteBody
): Promise<Note> {
  const res = await apiJson<NoteSaved>(`/contacts/${contactId}/notes`, {
    method: 'POST',
    body: JSON.stringify(data),
  });
  return res.note;
}

// Create unassigned note
export async function createUnassignedNote(
  data: NoteBody
): Promise<Note> {
  const res = await apiJson<NoteSaved>('/notes', {
    method: 'POST',
    body: JSON.stringify(data),
  });
  return res.note;
}

// Update note
export async function updateNote(
  id: string | number,
  data: NoteBody
): Promise<Note> {
  const res = await apiJson<NoteSaved>(`/notes/${id}`, {
    method: 'PUT',
    body: JSON.stringify(data),
  });
  return res.note;
}

// Delete note
export async function deleteNote(
  id: string | number
): Promise<void> {
  await apiJson(`/notes/${id}`, {
    method: 'DELETE',
  });
}
