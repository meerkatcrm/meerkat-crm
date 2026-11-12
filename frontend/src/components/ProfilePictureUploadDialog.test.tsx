import { describe, test, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import i18n from '../i18n/config';
import ProfilePictureUploadDialog from './ProfilePictureUploadDialog';
import * as client from '../api/client';

vi.mock('../api/client', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../api/client')>();
  return { ...actual, apiFetch: vi.fn() };
});

const envelope = (status: number, code: string, message: string, details: Record<string, string>) =>
  new Response(JSON.stringify({ error: { code, message, details } }), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });

async function fetchURL(url: string) {
  render(<ProfilePictureUploadDialog open onClose={() => {}} onUpload={async () => {}} />);
  fireEvent.change(screen.getByPlaceholderText('Paste image URL...'), { target: { value: url } });
  fireEvent.click(screen.getByRole('button', { name: 'Fetch' }));
}

describe('ProfilePictureUploadDialog image URL errors', () => {
  beforeEach(async () => {
    vi.resetAllMocks();
    await i18n.changeLanguage('en');
  });

  test('a refused URL shows a readable message, not the error details', async () => {
    vi.mocked(client.apiFetch).mockResolvedValue(
      envelope(503, 'EXTERNAL_SERVICE_ERROR', 'image proxy service error: Failed to fetch image', { service: 'image proxy' }),
    );
    await fetchURL('http://127.0.0.1/secret.png');
    expect(await screen.findByText('Failed to fetch image from URL')).toBeInTheDocument();
    expect(screen.queryByText('image proxy')).not.toBeInTheDocument();
  });

  test('the per-user limit has its own message', async () => {
    vi.mocked(client.apiFetch).mockResolvedValue(
      envelope(429, 'RATE_LIMIT_EXCEEDED', 'Rate limit exceeded. Please try again later.', {}),
    );
    await fetchURL('https://example.com/a.png');
    expect(await screen.findByText(/Too many images fetched from URLs/)).toBeInTheDocument();
  });
});
