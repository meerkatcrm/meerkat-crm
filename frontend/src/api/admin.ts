// Admin API calls for user management
import { apiJson } from './client';
import type { User, UsersListResponse, UserUpdateInput } from '../types';

// Get current authenticated user's information
export async function getCurrentUser(): Promise<User> {
  return apiJson('/users/me', {
    method: 'GET',
  });
}


// Get paginated list of all users (admin only)
export async function getUsers(
  page: number = 1,
  limit: number = 25
): Promise<UsersListResponse> {
  const params = new URLSearchParams({
    page: page.toString(),
    limit: limit.toString(),
  });

  return apiJson(`/admin/users?${params}`, {
    method: 'GET',
  });
}


// Update a user (admin only)
export async function updateUser(
  id: number,
  data: UserUpdateInput
): Promise<User> {
  return apiJson(`/admin/users/${id}`, {
    method: 'PATCH',
    body: JSON.stringify(data),
  });
}

// Trigger reminder emails manually (admin only)
export async function triggerReminders(): Promise<void> {
  await apiJson('/admin/trigger-reminders', {
    method: 'POST',
  });
}

/**
 * Delete a user (admin only)
 */
export async function deleteUser(id: number): Promise<void> {
  await apiJson(`/admin/users/${id}`, {
    method: 'DELETE',
  });
}
