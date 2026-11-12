import { describe, test, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import i18n from '../i18n/config';
import { SnackbarProvider } from '../context/SnackbarContext';
import { DateFormatProvider } from '../DateFormatProvider';
import WebhooksSettings from './WebhooksSettings';
import * as api from '../api/webhooks';

vi.mock('../api/webhooks', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../api/webhooks')>();
  return { ...actual, getWebhooks: vi.fn(), getWebhookDeliveries: vi.fn() };
});

const delivery = (over: Partial<api.WebhookDelivery>): api.WebhookDelivery => ({
  id: 1,
  event_type: 'contact.created',
  status_code: 204,
  error: null,
  attempts: 1,
  status: 'delivered',
  next_retry_at: null,
  created_at: '2030-01-01T00:00:00Z',
  ...over,
});

describe('WebhooksSettings deliveries', () => {
  beforeEach(async () => {
    vi.resetAllMocks();
    await i18n.changeLanguage('en');
  });

  test('shows delivered, retrying and gave-up deliveries', async () => {
    vi.mocked(api.getWebhooks).mockResolvedValue([
      {
        id: 1,
        name: 'Hook',
        url: 'https://example.com/h',
        events: ['contact.created'],
        is_active: true,
        created_at: '2030-01-01T00:00:00Z',
      },
    ]);
    vi.mocked(api.getWebhookDeliveries).mockResolvedValue([
      delivery({ id: 3, status: 'dead', status_code: 500, error: 'unexpected status 500', attempts: 5 }),
      delivery({
        id: 2,
        status: 'pending',
        status_code: null,
        error: 'timeout',
        next_retry_at: '2030-01-01T01:00:00Z',
      }),
      delivery({ id: 1 }),
      // first try still running: not a retry
      delivery({ id: 4, status: 'pending', status_code: null, attempts: 0, next_retry_at: '2030-01-01T00:00:00Z' }),
    ]);
    render(
      <DateFormatProvider>
        <SnackbarProvider>
          <WebhooksSettings />
        </SnackbarProvider>
      </DateFormatProvider>,
    );

    fireEvent.click(await screen.findByText('Recent Deliveries'));

    expect(await screen.findByText('Gave up')).toBeInTheDocument();
    expect(screen.getByText('Retrying')).toBeInTheDocument();
    expect(screen.getByText('Sending')).toBeInTheDocument();
    expect(screen.getByText('204')).toBeInTheDocument();
    expect(screen.getByText(/^Next try:/)).toBeInTheDocument();
  });
});
