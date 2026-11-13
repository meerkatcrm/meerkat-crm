import { format } from 'date-fns';
import { apiJson } from './client';
import { now } from '../utils/now';

export interface EmploymentHistoryEntry {
  ID: number;
  CreatedAt: string;
  UpdatedAt: string;
  contact_id: number;
  organization: string;
  department?: string;
  job_title?: string;
  role?: string;
  start_date?: string;
  end_date?: string;
  notes?: string;
}

export interface EmploymentHistoryFormData {
  organization: string;
  department?: string;
  job_title?: string;
  role?: string;
  start_date?: string;
  end_date?: string;
  notes?: string;
}

export interface EmploymentHistoryResponse {
  employment_history: EmploymentHistoryEntry[];
}

export async function getEmploymentHistory(
  contactId: number | string
): Promise<EmploymentHistoryResponse> {
  return apiJson(`/contacts/${contactId}/employment-history`);
}

export async function createEmploymentHistory(
  contactId: number | string,
  data: EmploymentHistoryFormData
): Promise<{ employment_history: EmploymentHistoryEntry }> {
  return apiJson(`/contacts/${contactId}/employment-history`, { method: 'POST', body: JSON.stringify(data) });
}

export async function updateEmploymentHistory(
  contactId: number | string,
  entryId: number,
  data: EmploymentHistoryFormData
): Promise<{ employment_history: EmploymentHistoryEntry }> {
  return apiJson(`/contacts/${contactId}/employment-history/${entryId}`, { method: 'PUT', body: JSON.stringify(data) });
}

export async function deleteEmploymentHistory(
  contactId: number | string,
  entryId: number
): Promise<void> {
  await apiJson(`/contacts/${contactId}/employment-history/${entryId}`, { method: 'DELETE' });
}

export async function endEmploymentHistory(
  contactId: number | string,
  entryId: number
): Promise<{ employment_history: EmploymentHistoryEntry }> {
  // The user's local today, which can differ from the server's UTC date.
  const today = format(now(), 'yyyy-MM-dd');
  return apiJson(`/contacts/${contactId}/employment-history/${entryId}/end?date=${today}`, { method: 'POST' });
}
