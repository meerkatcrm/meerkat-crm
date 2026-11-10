// Notes-related API calls
import { apiJson } from './client';

export interface Note {
  ID: number;
  content: string;
  date: string;
  contact_id?: number;
  CreatedAt: string;
  UpdatedAt: string;
}

export interface NotesResponse {
  notes: Note[];
  total?: number;
  page?: number;
  limit?: number;
}

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
): Promise<NotesResponse> {
  return apiJson(`/contacts/${contactId}/notes`);
}

// Get all unassigned notes
export async function getUnassignedNotes(
  params: GetNotesParams = {}
): Promise<NotesResponse> {
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
  data: { content: string; date: string; contact_id?: number }
): Promise<Note> {
  return apiJson(`/contacts/${contactId}/notes`, {
    method: 'POST',
    body: JSON.stringify(data),
  });
}

// Create unassigned note
export async function createUnassignedNote(
  data: { content: string; date: string }
): Promise<Note> {
  return apiJson('/notes', {
    method: 'POST',
    body: JSON.stringify(data),
  });
}

// Update note
export async function updateNote(
  id: string | number,
  data: { content: string; date: string; contact_id?: number }
): Promise<Note> {
  return apiJson(`/notes/${id}`, {
    method: 'PUT',
    body: JSON.stringify(data),
  });
}

// Delete note
export async function deleteNote(
  id: string | number
): Promise<void> {
  await apiJson(`/notes/${id}`, {
    method: 'DELETE',
  });
}
