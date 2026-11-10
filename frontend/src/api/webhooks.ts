import { apiJson } from './client';

export interface Webhook {
  id: number;
  name: string;
  url: string;
  events: string[];
  is_active: boolean;
  created_at: string;
}

export interface WebhookCreateResponse extends Webhook {
  secret: string;
}

export interface WebhookDelivery {
  id: number;
  event_type: string;
  status_code: number | null;
  error: string | null;
  attempts: number;
  created_at: string;
}

export async function getWebhooks(): Promise<Webhook[]> {
  const data = await apiJson<{ webhooks?: Webhook[] } | undefined>('/webhooks', {
    errorMessage: 'Unable to load webhooks.',
  });
  return data?.webhooks || [];
}

export async function createWebhook(input: {
  name: string;
  url: string;
  events: string[];
  is_active: boolean;
}): Promise<WebhookCreateResponse> {
  return apiJson<WebhookCreateResponse>('/webhooks', {
    method: 'POST',
    body: JSON.stringify(input),
    errorMessage: 'Unable to create webhook.',
  });
}

export async function updateWebhook(
  id: number,
  input: { name: string; url: string; events: string[]; is_active: boolean }
): Promise<Webhook> {
  return apiJson<Webhook>(`/webhooks/${id}`, {
    method: 'PUT',
    body: JSON.stringify(input),
    errorMessage: 'Unable to update webhook.',
  });
}

export async function deleteWebhook(id: number): Promise<void> {
  await apiJson(`/webhooks/${id}`, {
      method: 'DELETE',
      errorMessage: 'Unable to delete webhook.',
    });
  }

  export async function testWebhook(id: number): Promise<{ delivery: WebhookDelivery }> {
    return apiJson<{ delivery: WebhookDelivery }>(`/webhooks/${id}/test`, {
      method: 'POST',
      errorMessage: 'Unable to test webhook.',
    });
  }

  export async function getWebhookDeliveries(id: number): Promise<WebhookDelivery[]> {
    const data = await apiJson<{ deliveries?: WebhookDelivery[] } | undefined>(`/webhooks/${id}/deliveries`, {
      errorMessage: 'Unable to load deliveries.',
    });
    return data?.deliveries || [];
  }
