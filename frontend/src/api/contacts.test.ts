import { describe, test, expect, vi, beforeEach, afterEach } from 'vitest';
import { updateContact, type Contact } from './contacts';

const fetchMock = vi.fn();

describe('updateContact', () => {
  beforeEach(() => {
    fetchMock.mockReset();
    fetchMock.mockResolvedValue(new Response(JSON.stringify({ ID: 1 }), { status: 200 }));
    vi.stubGlobal('fetch', fetchMock);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
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

    const [url, init] = fetchMock.mock.calls[0];
    expect(String(url)).toMatch(/\/contacts\/1$/);
    expect(init?.method).toBe('PUT');
    expect(JSON.parse(String(init?.body))).toEqual({ firstname: 'Erin', nickname: 'E' });
  });
});
