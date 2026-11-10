import { apiJson } from './client';

export interface ApiToken {
  id: number;
  name: string;
  created_at: string;
  last_used_at: string | null;
  revoked_at: string | null;
}

export interface ApiTokenCreateResponse extends ApiToken {
  token: string;
}

export interface ApiTokensListResponse {
  tokens: ApiToken[];
}

export async function getApiTokens(): Promise<ApiTokensListResponse> {
  const data = await apiJson<{ tokens?: ApiToken[] } | undefined>('/api-tokens', {
    errorMessage: 'Unable to load API tokens.',
  });
  return { tokens: data?.tokens || [] };
}

export async function createApiToken(name: string): Promise<ApiTokenCreateResponse> {
  return apiJson<ApiTokenCreateResponse>('/api-tokens', {
    method: 'POST',
    body: JSON.stringify({ name }),
    errorMessage: 'Unable to create API token.',
  });
}

export async function revokeApiToken(id: number): Promise<void> {
  await apiJson(`/api-tokens/${id}`, {
      method: 'DELETE',
      errorMessage: 'Unable to revoke API token.',
    });
  }
