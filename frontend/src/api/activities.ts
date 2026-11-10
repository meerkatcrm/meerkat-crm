// Activities-related API calls
import { apiJson } from './client';

export interface ActivityContact {
  ID: number;
  firstname: string;
  lastname: string;
  nickname?: string;
}

export interface Activity {
  ID: number;
  title: string;
  description?: string;
  location?: string;
  date: string;
  CreatedAt: string;
  UpdatedAt: string;
  contacts?: ActivityContact[];
}

export interface ActivitiesResponse {
  activities: Activity[];
  total: number;
  page: number;
  limit: number;
}

export interface GetActivitiesParams {
  page?: number;
  limit?: number;
  includeContacts?: boolean;
  search?: string;
  fromDate?: string;
  toDate?: string;
}

// Get all activities
export async function getActivities(
  params: GetActivitiesParams
): Promise<ActivitiesResponse> {
  const { page = 1, limit = 25, includeContacts = false } = params;
  const search = params.search?.trim();

  const queryParams = new URLSearchParams({
    page: page.toString(),
    limit: limit.toString(),
  });

  if (includeContacts) {
    queryParams.append('include', 'contacts');
  }

  if (search) {
    queryParams.append('search', search);
  }

  if (params.fromDate) {
    queryParams.append('fromDate', params.fromDate);
  }

  if (params.toDate) {
    queryParams.append('toDate', params.toDate);
  }

  return apiJson(`/activities?${queryParams.toString()}`);
}

// Get activities for a contact
export async function getContactActivities(
  contactId: string | number
): Promise<{ activities: Activity[] }> {
  return apiJson(`/contacts/${contactId}/activities`);
}

// Get single activity
export async function getActivity(
  id: string | number
): Promise<Activity> {
  return apiJson(`/activities/${id}`);
}

// Create activity
export async function createActivity(
  data: {
    title: string;
    description: string;
    location: string;
    date: string;
    contact_ids: number[];
  }
): Promise<Activity> {
  return apiJson('/activities', {
    method: 'POST',
    body: JSON.stringify(data),
  });
}

// Update activity
export async function updateActivity(
  id: string | number,
  data: {
    title?: string;
    description?: string;
    location?: string;
    date?: string;
    contact_ids?: number[];
  }
): Promise<Activity> {
  return apiJson(`/activities/${id}`, {
    method: 'PUT',
    body: JSON.stringify(data),
  });
}

// Delete activity
export async function deleteActivity(
  id: string | number
): Promise<void> {
  await apiJson(`/activities/${id}`, {
    method: 'DELETE',
  });
}
