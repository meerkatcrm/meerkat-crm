// API client with authentication and error handling
// Uses httpOnly cookies for authentication (credentials: 'include')
import { extractDetailMessage } from './errorHandling';

const API_SERVER_URL = import.meta.env.REACT_APP_API_URL || '';
export const API_BASE_URL = `${API_SERVER_URL}/api/v1`;

// Default request timeout in milliseconds (30 seconds)
const DEFAULT_TIMEOUT = parseInt(import.meta.env.REACT_APP_REQUEST_TIMEOUT || '30000', 10);

// Backend error response structure
interface BackendErrorResponse {
  error: {
    code: string;
    message: string;
    details?: Record<string, string>;
  };
  request_id?: string;
  timestamp?: string;
}

// Custom API error class with detailed information
export class ApiError extends Error {
  code: string;
  status: number;
  details?: Record<string, string>;
  requestId?: string;

  constructor(
    message: string,
    code: string = 'UNKNOWN_ERROR',
    status: number = 500,
    details?: Record<string, string>,
    requestId?: string
  ) {
    super(message);
    this.name = 'ApiError';
    this.code = code;
    this.status = status;
    this.details = details;
    this.requestId = requestId;
  }

  // Get a user-friendly error message including field details
  getDisplayMessage(): string {
    const fieldEntries = Object.entries(this.details ?? {}).filter(([key]) => key !== 'service');
    if (fieldEntries.length > 0) {
      return fieldEntries.map(([, msg]) => `${msg}`).join('. ');
    }
    return this.message;
  }
}

/**
 * Turns an error response into an ApiError.
 *
 * By default the message is the backend envelope's `error.message` and field
 * errors stay in `details` (see getDisplayMessage). Pass `errorMessage` for
 * callers that display `err.message` directly: the message then becomes the
 * most specific text the response offers (validation details, the server
 * message, a plain-text body), falling back to `errorMessage`.
 */
export async function parseErrorResponse(response: Response, errorMessage?: string): Promise<ApiError> {
  let raw = '';
  try {
    raw = await response.text();
  } catch {
    // Unreadable body, fall back below
  }

  let data: unknown = raw;
  try {
    data = JSON.parse(raw);
  } catch {
    // Not JSON, keep the text
  }

  let message: string | undefined;
  let code = 'UNKNOWN_ERROR';
  let details: Record<string, string> | undefined;
  let requestId: string | undefined;

  const nonEmpty = (value: unknown): string | undefined =>
    typeof value === 'string' && value.trim().length > 0 ? value.trim() : undefined;

  if (data && typeof data === 'object') {
    const body = data as Partial<BackendErrorResponse> & { error?: unknown; message?: unknown };
    if (body.error && typeof body.error === 'object') {
      const envelope = body.error as BackendErrorResponse['error'];
      code = envelope.code;
      details = envelope.details;
      requestId = body.request_id;
      message = errorMessage === undefined
        ? envelope.message
        : extractDetailMessage(envelope.details) ?? nonEmpty(envelope.message);
    } else if (errorMessage !== undefined) {
      // Simple formats: {"error": "message"} and {"message": "message"}
      message = nonEmpty(body.error) ?? nonEmpty(body.message);
    }
  } else if (errorMessage !== undefined) {
    message = nonEmpty(data);
  }

  return new ApiError(
    message ?? errorMessage ?? (response.statusText || 'An error occurred'),
    code,
    response.status,
    details,
    requestId
  );
}

export interface ApiRequestInit extends RequestInit {
  /** Request timeout in milliseconds. */
  timeout?: number;
  /**
   * Leave a 401 to the caller instead of logging out and redirecting to /login.
   * For requests where "not signed in" is an expected answer: the session
   * probe, login and registration.
   */
  skipAuthRedirect?: boolean;
  /** See parseErrorResponse. Only used by apiJson. */
  errorMessage?: string;
}

// Centralized fetch wrapper that handles session expiration and timeouts
export async function apiFetch(
  url: string,
  options: ApiRequestInit = {},
  timeout: number = options.timeout ?? DEFAULT_TIMEOUT
): Promise<Response> {
  const {
    signal: callerSignal,
    skipAuthRedirect = false,
    timeout: _timeout,
    errorMessage: _errorMessage,
    ...init
  } = options;

  // One controller aborts for both the timeout and the caller's own signal.
  const controller = new AbortController();
  let timedOut = false;
  const timeoutId = setTimeout(() => {
    timedOut = true;
    controller.abort();
  }, timeout);
  const forwardAbort = () => controller.abort(callerSignal?.reason);
  if (callerSignal?.aborted) {
    forwardAbort();
  } else {
    callerSignal?.addEventListener('abort', forwardAbort);
  }

  try {
    const response = await fetch(url, {
      ...init,
      credentials: 'include', // Send httpOnly auth cookie
      signal: controller.signal,
    });

    // Check if session has expired (401 Unauthorized)
    if (response.status === 401 && !skipAuthRedirect) {
      // Session is invalid or expired - logout and redirect
      localStorage.removeItem('user_info');
      window.location.href = '/login';
      throw new Error('Session expired. Please login again.');
    }

    return response;
  } catch (error) {
    // An abort is a timeout only when the timer fired; the caller's own abort passes through.
    if (timedOut) {
      throw new Error(`Request timeout after ${timeout / 1000} seconds. Please check your connection.`);
    }

    throw error;
  } finally {
    clearTimeout(timeoutId);
    callerSignal?.removeEventListener('abort', forwardAbort);
  }
}

/**
 * Requests `path` (relative to the API base URL) and returns the parsed JSON
 * body. A non-2xx response is thrown as an ApiError; an empty body (204)
 * resolves to undefined. JSON headers are sent unless `headers` is given or
 * the body is FormData.
 */
export async function apiJson<T = void>(path: string, init: ApiRequestInit = {}): Promise<T> {
  const isFormData = typeof FormData !== 'undefined' && init.body instanceof FormData;
  const response = await apiFetch(`${API_BASE_URL}${path}`, {
    ...init,
    headers: init.headers ?? (isFormData ? undefined : getAuthHeaders()),
  });

  if (!response.ok) {
    throw await parseErrorResponse(response, init.errorMessage);
  }

  const raw = await response.text();
  return (raw ? JSON.parse(raw) : undefined) as T;
}

// Helper to create request headers (no longer includes Authorization - using cookies)
export function getAuthHeaders(): HeadersInit {
  return {
    'Content-Type': 'application/json',
  };
}
