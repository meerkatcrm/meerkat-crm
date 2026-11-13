import { describe, test, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
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

describe('ProfilePictureUploadDialog remove photo', () => {
  beforeEach(async () => {
    vi.resetAllMocks();
    await i18n.changeLanguage('en');
  });

  test('there is no remove button when the contact has no photo', () => {
    render(<ProfilePictureUploadDialog open onClose={() => {}} onUpload={async () => {}} />);
    expect(screen.queryByRole('button', { name: 'Remove photo' })).not.toBeInTheDocument();
  });

  test('removing the photo closes the dialog', async () => {
    const onRemove = vi.fn().mockResolvedValue(true);
    const onClose = vi.fn();
    render(<ProfilePictureUploadDialog open onClose={onClose} onUpload={async () => {}} onRemove={onRemove} />);

    fireEvent.click(screen.getByRole('button', { name: 'Remove photo' }));

    await waitFor(() => expect(onClose).toHaveBeenCalled());
    expect(onRemove).toHaveBeenCalledTimes(1);
  });

  test('backing out of the confirmation keeps the dialog open', async () => {
    const onRemove = vi.fn().mockResolvedValue(false);
    const onClose = vi.fn();
    render(<ProfilePictureUploadDialog open onClose={onClose} onUpload={async () => {}} onRemove={onRemove} />);

    fireEvent.click(screen.getByRole('button', { name: 'Remove photo' }));

    await waitFor(() => expect(onRemove).toHaveBeenCalled());
    await waitFor(() => expect(screen.getByRole('button', { name: 'Remove photo' })).toBeEnabled());
    expect(onClose).not.toHaveBeenCalled();
  });

  test('a failed removal shows an error and keeps the dialog open', async () => {
    const onRemove = vi.fn().mockRejectedValue(new Error('boom'));
    const onClose = vi.fn();
    render(<ProfilePictureUploadDialog open onClose={onClose} onUpload={async () => {}} onRemove={onRemove} />);

    fireEvent.click(screen.getByRole('button', { name: 'Remove photo' }));

    expect(await screen.findByText('Failed to remove profile picture')).toBeInTheDocument();
    expect(onClose).not.toHaveBeenCalled();
  });
});
