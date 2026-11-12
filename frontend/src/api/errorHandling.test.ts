import { describe, test, expect } from 'vitest';
import { extractDetailMessage } from './errorHandling';

describe('extractDetailMessage', () => {
  test('returns null for null, undefined, and non-objects', () => {
    expect(extractDetailMessage(null)).toBeNull();
    expect(extractDetailMessage(undefined)).toBeNull();
  });

  test('prefers the reason field over other values', () => {
    expect(extractDetailMessage({ reason: 'Rate limit exceeded', email: 'Invalid email' })).toBe(
      'Rate limit exceeded'
    );
  });

  test('joins string values from the details object', () => {
    expect(extractDetailMessage({ email: 'Email already taken', username: 'Username too short' })).toBe(
      'Email already taken Username too short'
    );
  });

  test('collects strings from array values', () => {
    expect(extractDetailMessage({ fields: ['First error', 'Second error'] })).toBe(
      'First error Second error'
    );
  });

  test('ignores empty strings and non-string values', () => {
    expect(extractDetailMessage({ a: '', b: 42, c: { nested: true } })).toBeNull();
  });

  test('ignores the service name of an external service error', () => {
    expect(extractDetailMessage({ service: 'CardDAV' })).toBeNull();
    expect(extractDetailMessage({ service: 'CardDAV', base_url: 'Invalid URL' })).toBe('Invalid URL');
  });
});
