import { useState, useEffect, useCallback } from 'react';
import { API_BASE_URL, apiFetch } from '../api/client';

/** Which sign-in methods this instance offers (GET /auth/config). */
export interface AuthConfig {
  oidcEnabled: boolean;
  oidcProviderName: string;
  passwordLoginEnabled: boolean;
  registrationEnabled: boolean;
}

// Until the server answers (or if it cannot), behave as a plain password instance:
// hiding the password form on a failed request would lock users out.
export const DEFAULT_AUTH_CONFIG: AuthConfig = {
  oidcEnabled: false,
  oidcProviderName: 'SSO',
  passwordLoginEnabled: true,
  registrationEnabled: true,
};

export function parseAuthConfig(data: unknown): AuthConfig {
  if (!data || typeof data !== 'object') return DEFAULT_AUTH_CONFIG;
  const d = data as Record<string, unknown>;
  const oidcEnabled = d.oidc_enabled === true;
  return {
    oidcEnabled,
    oidcProviderName: typeof d.oidc_provider_name === 'string' && d.oidc_provider_name ? d.oidc_provider_name : 'SSO',
    // Password sign-in is only ever turned off when SSO is there to replace it.
    passwordLoginEnabled: !(oidcEnabled && d.password_login_enabled === false),
    registrationEnabled: d.registration_enabled !== false,
  };
}

export function useAuthConfig() {
  const [data, setData] = useState<AuthConfig>(DEFAULT_AUTH_CONFIG);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<Error | null>(null);

  const refetch = useCallback(async () => {
    setLoading(true);
    try {
      // Asked on the login page, before there is a session
      const res = await apiFetch(`${API_BASE_URL}/auth/config`, { skipAuthRedirect: true });
      if (!res.ok) throw new Error(`auth config: HTTP ${res.status}`);
      setData(parseAuthConfig(await res.json()));
      setError(null);
    } catch (err) {
      // Best-effort: keep the defaults so password sign-in stays available
      setError(err instanceof Error ? err : new Error(String(err)));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    refetch();
  }, [refetch]);

  return { data, loading, error, refetch };
}
