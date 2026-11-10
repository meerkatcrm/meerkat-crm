// Shared error handling utilities for API responses

export type ErrorDetails = Record<string, unknown> | undefined | null;

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

  Object.values(normalized).forEach(value => {
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
