// Types shared between the admin API module and the users page

/**
 * User profile information
 */
export interface User {
  id: number;
  email: string;
  username: string;
  language: string;
  is_admin: boolean;
  created_at: string;
  updated_at: string;
  // Present only on the /users/me response (CurrentUserResponse), not in admin lists.
  custom_field_names?: string[];
  // null means the user has never configured it; apply DEFAULT_ENABLED_CONTACT_FIELDS.
  enabled_contact_fields?: string[] | null;
  // When true, the contact detail page hides empty fields until the user expands them.
  hide_empty_contact_fields?: boolean;
  // The contact marked as the user themself ("me"); null when none is.
  self_contact_id?: number | null;
}

/**
 * Paginated list of users for admin
 */
export interface UsersListResponse {
  users: User[];
  total: number;
  page: number;
  limit: number;
  total_pages: number;
}

/**
 * Input for updating a user
 */
export interface UserUpdateInput {
  username?: string;
  email?: string;
  password?: string;
  is_admin?: boolean;
}
