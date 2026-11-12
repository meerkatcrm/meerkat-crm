// src/auth.ts
// Cookie-based JWT auth service for meerkat crm frontend
// Token is stored in httpOnly cookie (not accessible from JS for security)
// User info is cached in localStorage for UI purposes

import { API_BASE_URL, apiFetch } from './api/client';

export { API_BASE_URL };

const USER_INFO_KEY = 'user_info';

export interface LoginResponse {
  language?: string;
  date_format?: string;
}

export interface UserInfo {
  user_id: number;
  username: string;
  is_admin: boolean;
  // Public demo instance: the server refuses uploads and password changes.
  demo_mode?: boolean;
}

export async function loginUser(identifier: string, password: string): Promise<LoginResponse> {
  const response = await apiFetch(`${API_BASE_URL}/login`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ identifier, password }),
    // Wrong credentials are a 401: an answer for the login form, not an expired session.
    skipAuthRedirect: true,
  });
  if (!response.ok) {
    throw new Error('Login failed');
  }
  const data = await response.json();

  // Fetch user info and cache it (since we can't read the httpOnly cookie)
  // Without it isAuthenticated() stays false, and the login page would show no error.
  if (!(await fetchAndCacheUserInfo())) {
    throw new Error('Login failed');
  }

  return {
    language: data.language,
    date_format: data.date_format,
  };
}

// Fetch current user info from the server and cache it
export async function fetchAndCacheUserInfo(): Promise<(UserInfo & LoginResponse) | null> {
  try {
    // This is the "am I signed in?" probe, so a 401 just means no.
    const response = await apiFetch(`${API_BASE_URL}/users/me`, { skipAuthRedirect: true });
    if (!response.ok) {
      return null;
    }
    const data = await response.json();
    const userInfo: UserInfo = {
      user_id: data.id,
      username: data.username,
      is_admin: data.is_admin || false,
      demo_mode: data.demo_mode || false,
    };
    localStorage.setItem(USER_INFO_KEY, JSON.stringify(userInfo));
    // The preferences are returned for the caller to apply, not cached with the identity.
    return { ...userInfo, language: data.language, date_format: data.date_format };
  } catch {
    return null;
  }
}

// Returns null - token is in httpOnly cookie (not accessible from JS)
// Use isAuthenticated() to check login status
export function getToken(): string | null {
  // Return a placeholder if user info exists (indicates logged in)
  // This maintains compatibility with existing code that checks for token presence
  const userInfo = localStorage.getItem(USER_INFO_KEY);
  return userInfo ? 'cookie-auth' : null;
}

// Check if user is authenticated (has cached user info)
export function isAuthenticated(): boolean {
  return localStorage.getItem(USER_INFO_KEY) !== null;
}

export async function logoutUser() {
  try {
    await apiFetch(`${API_BASE_URL}/logout`, { method: 'POST', skipAuthRedirect: true });
  } catch {
    // Ignore errors - clear local state anyway
  }
  localStorage.removeItem(USER_INFO_KEY);
}

export async function logoutAndRedirect() {
  await logoutUser();
  window.location.href = '/login';
}

interface DecodedToken {
  user_id: number;
  username: string;
  is_admin: boolean;
  exp: number;
}

// Returns cached user info (previously decoded from token)
export function decodeToken(): DecodedToken | null {
  const userInfoStr = localStorage.getItem(USER_INFO_KEY);
  if (!userInfoStr) {
    return null;
  }

  try {
    const userInfo: UserInfo = JSON.parse(userInfoStr);
    return {
      user_id: userInfo.user_id,
      username: userInfo.username,
      is_admin: userInfo.is_admin,
      exp: 0, // Expiry handled server-side via cookie
    };
  } catch {
    return null;
  }
}

// Check if the current user is an admin based on cached user info
export function isAdmin(): boolean {
  const decoded = decodeToken();
  return decoded?.is_admin || false;
}

/**
 * True on a public demo instance, where the server refuses uploads and password
 * changes, so the UI hides those controls. Read from the cached /users/me info.
 */
export function isDemoMode(): boolean {
  try {
    const userInfo: UserInfo = JSON.parse(localStorage.getItem(USER_INFO_KEY) ?? 'null');
    return userInfo?.demo_mode || false;
  } catch {
    return false;
  }
}
