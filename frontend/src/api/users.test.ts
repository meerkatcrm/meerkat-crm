import { describe, test, expect, vi, beforeEach, afterEach } from 'vitest';
import { setSelfContact } from './users';

const fetchMock = vi.fn();

describe('setSelfContact', () => {
  beforeEach(() => {
    fetchMock.mockReset();
    fetchMock.mockImplementation(() => Promise.resolve(new Response('{}', { status: 200 })));
    vi.stubGlobal('fetch', fetchMock);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  test('marks a contact with a PUT naming it', async () => {
    await expect(setSelfContact(7)).resolves.toBe(7);

    const [url, init] = fetchMock.mock.calls[0];
    expect(String(url)).toMatch(/\/users\/self-contact$/);
    expect(init?.method).toBe('PUT');
    expect(JSON.parse(String(init?.body))).toEqual({ contact_id: 7 });
  });

  test('clears the mark with a DELETE, never with an empty PUT', async () => {
    await expect(setSelfContact(null)).resolves.toBeNull();

    const [url, init] = fetchMock.mock.calls[0];
    expect(String(url)).toMatch(/\/users\/self-contact$/);
    expect(init?.method).toBe('DELETE');
  });
});
