import { describe, test, expect, vi, beforeEach, afterEach } from 'vitest';
import { ApiError, apiFetch, apiJson, parseErrorResponse } from './client';

describe('ApiError.getDisplayMessage', () => {
  test('returns the message when there are no details', () => {
    const err = new ApiError('Something went wrong', 'INTERNAL', 500);
    expect(err.getDisplayMessage()).toBe('Something went wrong');
  });

  test('joins field-level details when present', () => {
    const err = new ApiError('Validation failed', 'VALIDATION', 400, {
      email: 'Email is invalid',
      password: 'Password too weak',
    });
    expect(err.getDisplayMessage()).toBe('Email is invalid. Password too weak');
  });

  test('returns the message for an empty details object', () => {
    const err = new ApiError('Validation failed', 'VALIDATION', 400, {});
    expect(err.getDisplayMessage()).toBe('Validation failed');
  });

  test('shows the message of an external service error, not the service name', () => {
    const err = new ApiError('CardDAV service error: server answered with an error', 'EXTERNAL_SERVICE_ERROR', 503, {
      service: 'CardDAV',
    });
    expect(err.getDisplayMessage()).toBe('CardDAV service error: server answered with an error');
  });

  test('parseErrorResponse with a fallback reads the message of an external service error', async () => {
    const response = new Response(
      JSON.stringify({ error: { code: 'EXTERNAL_SERVICE_ERROR', message: 'CardDAV service error: server answered with an error', details: { service: 'CardDAV' } } }),
      { status: 503 },
    );
    const err = await parseErrorResponse(response, 'Unable to discover address books.');
    expect(err.message).toBe('CardDAV service error: server answered with an error');
  });
});

describe('parseErrorResponse', () => {
  test('parses a structured backend error', async () => {
    const response = new Response(
      JSON.stringify({
        error: { code: 'NOT_FOUND', message: 'Contact not found', details: { id: '42' } },
        request_id: 'req-123',
      }),
      { status: 404 }
    );

    const err = await parseErrorResponse(response);
    expect(err).toBeInstanceOf(ApiError);
    expect(err.message).toBe('Contact not found');
    expect(err.code).toBe('NOT_FOUND');
    expect(err.status).toBe(404);
    expect(err.details).toEqual({ id: '42' });
    expect(err.requestId).toBe('req-123');
  });

  test('falls back to status text for a non-JSON body', async () => {
    const response = new Response('<html>Bad Gateway</html>', {
      status: 502,
      statusText: 'Bad Gateway',
    });

    const err = await parseErrorResponse(response);
    expect(err.message).toBe('Bad Gateway');
    expect(err.code).toBe('UNKNOWN_ERROR');
    expect(err.status).toBe(502);
  });

  test('falls back to a generic message when status text is empty', async () => {
    const response = new Response('not json', { status: 500 });

    const err = await parseErrorResponse(response);
    expect(err.message).toBe('An error occurred');
    expect(err.status).toBe(500);
  });
});

// Callers that show err.message directly pass a fallback and get the most specific text.
describe('parseErrorResponse with an errorMessage', () => {
  const jsonResponse = (body: unknown, status: number) => new Response(JSON.stringify(body), { status });

  test('puts the field detail of a structured backend error into the message', async () => {
    const err = await parseErrorResponse(
      jsonResponse(
        { error: { code: 'VALIDATION', message: 'Validation failed', details: { email: 'Email already registered' } }, request_id: 'r1' },
        400
      ),
      'fallback'
    );
    expect(err).toBeInstanceOf(ApiError);
    expect(err.message).toBe('Email already registered');
    expect(err.code).toBe('VALIDATION');
    expect(err.status).toBe(400);
    expect(err.details).toEqual({ email: 'Email already registered' });
    expect(err.requestId).toBe('r1');
  });

  test('uses the error message when there are no details', async () => {
    const err = await parseErrorResponse(jsonResponse({ error: { code: 'NOT_FOUND', message: 'Contact not found' } }, 404), 'fallback');
    expect(err.message).toBe('Contact not found');
  });

  test('handles the simple string error format', async () => {
    const err = await parseErrorResponse(jsonResponse({ error: 'Something broke' }, 500), 'fallback');
    expect(err.message).toBe('Something broke');
  });

  test('uses a top-level message field when present', async () => {
    const err = await parseErrorResponse(jsonResponse({ message: 'Too many requests' }, 429), 'fallback');
    expect(err.message).toBe('Too many requests');
  });

  test('uses a plain-text error body as the message', async () => {
    const err = await parseErrorResponse(new Response('Bad Gateway', { status: 502 }), 'fallback');
    expect(err.message).toBe('Bad Gateway');
    expect(err.status).toBe(502);
  });

  test('falls back to the provided message for an empty error body', async () => {
    const err = await parseErrorResponse(new Response('', { status: 500 }), 'Request failed');
    expect(err.message).toBe('Request failed');
    expect(err.code).toBe('UNKNOWN_ERROR');
  });
});

describe('apiFetch / apiJson', () => {
  const fetchMock = vi.fn();
  const location = { href: '/contacts' };

  beforeEach(() => {
    fetchMock.mockReset();
    location.href = '/contacts';
    localStorage.setItem('user_info', '{}');
    vi.stubGlobal('fetch', fetchMock);
    vi.stubGlobal('location', location);
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  test('apiJson requests the path under the API base with cookies and JSON headers', async () => {
    fetchMock.mockResolvedValue(new Response(JSON.stringify({ id: 1 }), { status: 200 }));
    await expect(apiJson<{ id: number }>('/things/1')).resolves.toEqual({ id: 1 });
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toMatch(/\/api\/v1\/things\/1$/);
    expect(init).toMatchObject({ credentials: 'include', headers: { 'Content-Type': 'application/json' } });
    // Options of the wrapper are not passed on to fetch
    expect(init).not.toHaveProperty('errorMessage');
    expect(init).not.toHaveProperty('skipAuthRedirect');
    expect(init).not.toHaveProperty('timeout');
  });

  test('apiJson leaves the content type of a FormData body to the browser', async () => {
    fetchMock.mockResolvedValue(new Response('{}', { status: 200 }));
    await apiJson('/upload', { method: 'POST', body: new FormData() });
    expect(fetchMock.mock.calls[0][1].headers).toBeUndefined();
  });

  test('apiJson resolves undefined for 204 and other empty bodies', async () => {
    fetchMock.mockResolvedValueOnce(new Response(null, { status: 204 }));
    await expect(apiJson('/things/1', { method: 'DELETE' })).resolves.toBeUndefined();
    fetchMock.mockResolvedValueOnce(new Response('', { status: 200 }));
    await expect(apiJson('/things/1')).resolves.toBeUndefined();
  });

  test('apiJson throws an ApiError for an error response', async () => {
    const body = { error: { code: 'VALIDATION', message: 'Validation failed', details: { name: 'Name is required' } } };
    fetchMock.mockImplementation(async () => new Response(JSON.stringify(body), { status: 400 }));

    const plain = await apiJson('/things').catch((e) => e);
    expect(plain).toBeInstanceOf(ApiError);
    expect(plain.message).toBe('Validation failed');
    expect(plain.getDisplayMessage()).toBe('Name is required');

    const specific = await apiJson('/things', { errorMessage: 'Unable to load things.' }).catch((e) => e);
    expect(specific).toBeInstanceOf(ApiError);
    expect(specific.message).toBe('Name is required');
    expect(specific.status).toBe(400);
  });

  test('a 401 logs out and redirects to the login page', async () => {
    fetchMock.mockResolvedValue(new Response('', { status: 401 }));
    await expect(apiFetch('/api/v1/contacts')).rejects.toThrow('Session expired');
    expect(location.href).toBe('/login');
    expect(localStorage.getItem('user_info')).toBeNull();
  });

  test('skipAuthRedirect hands a 401 back to the caller', async () => {
    fetchMock.mockResolvedValue(new Response('', { status: 401 }));
    const response = await apiFetch('/api/v1/users/me', { skipAuthRedirect: true });
    expect(response.status).toBe(401);
    expect(location.href).toBe('/contacts');
    expect(localStorage.getItem('user_info')).toBe('{}');
  });

  // A fetch that only settles when its signal aborts, like the real one
  const hangUntilAborted = (_url: string, init: RequestInit) =>
    new Promise((_resolve, reject) => {
      init.signal!.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError')));
    });

  test('a caller-supplied signal still aborts the request', async () => {
    fetchMock.mockImplementation(hangUntilAborted);
    const controller = new AbortController();
    const request = apiFetch('/api/v1/contacts', { signal: controller.signal });
    controller.abort();
    // The caller's own abort is not reported as a timeout
    await expect(request).rejects.toMatchObject({ name: 'AbortError' });
  });

  test('an already aborted signal aborts immediately', async () => {
    fetchMock.mockImplementation((_url: string, init: RequestInit) =>
      init.signal!.aborted ? Promise.reject(new DOMException('Aborted', 'AbortError')) : new Promise(() => {})
    );
    const controller = new AbortController();
    controller.abort();
    await expect(apiFetch('/api/v1/contacts', { signal: controller.signal })).rejects.toMatchObject({ name: 'AbortError' });
  });

  test('the timeout still applies when the caller passes a signal', async () => {
    vi.useFakeTimers();
    fetchMock.mockImplementation(hangUntilAborted);
    const request = apiFetch('/api/v1/contacts', { signal: new AbortController().signal, timeout: 5000 });
    const outcome = expect(request).rejects.toThrow('Request timeout after 5 seconds');
    await vi.advanceTimersByTimeAsync(5000);
    await outcome;
  });
});
