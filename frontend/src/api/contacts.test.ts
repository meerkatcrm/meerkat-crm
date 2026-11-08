import { describe, test, expect, vi, beforeEach } from 'vitest';
import { apiFetch } from './client';
import { updateContact, type Contact } from './contacts';

vi.mock('./client', async importOriginal => ({
  ...(await importOriginal<typeof import('./client')>()),
  apiFetch: vi.fn(),
}));

describe('updateContact', () => {
  beforeEach(() => {
    vi.mocked(apiFetch).mockReset();
    vi.mocked(apiFetch).mockResolvedValue(new Response(JSON.stringify({ ID: 1 }), { status: 200 }));
  });

  test('does not send the employment fields, so a stale copy cannot overwrite a newer job', async () => {
    const stale: Partial<Contact> = {
      firstname: 'Erin',
      nickname: 'E',
      organization: 'Acme',
      department: 'R&D',
      job_title: 'Engineer',
      role: 'Lead',
    };

    await updateContact(1, stale);

    const [url, init] = vi.mocked(apiFetch).mock.calls[0];
    expect(String(url)).toMatch(/\/contacts\/1$/);
    expect(init?.method).toBe('PUT');
    expect(JSON.parse(String(init?.body))).toEqual({ firstname: 'Erin', nickname: 'E' });
  });
});
