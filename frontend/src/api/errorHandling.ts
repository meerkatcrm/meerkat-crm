// Shared error handling utilities for API responses

export type ErrorDetails = Record<string, unknown> | undefined | null;

// Detail keys that name something instead of describing the error. The backend's external
// service errors carry {"service": "CardDAV"}; shown alone, that hides the real message.
const NON_MESSAGE_KEYS = new Set(['service']);

export function extractDetailMessage(details: ErrorDetails): string | null {
  if (!details || typeof details !== 'object') {
    return null;
  }

  const normalized = details as Record<string, unknown>;

  const reason = normalized.reason;
  if (typeof reason === 'string' && reason.trim().length > 0) {
    return reason.trim();
  }

  const messages: string[] = [];

  Object.entries(normalized).forEach(([key, value]) => {
    if (NON_MESSAGE_KEYS.has(key)) {
      return;
    }
    if (typeof value === 'string' && value.trim().length > 0) {
      messages.push(value.trim());
      return;
    }

    if (Array.isArray(value)) {
      value.forEach(item => {
        if (typeof item === 'string' && item.trim().length > 0) {
          messages.push(item.trim());
        }
      });
    }
  });

  if (messages.length === 0) {
    return null;
  }

  return messages.join(' ');
}
