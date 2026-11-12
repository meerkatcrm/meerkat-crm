import { describe, expect, test } from 'vitest';
import { thumbnailSrc } from './thumbnail';

describe('thumbnailSrc', () => {
  test('no thumbnail gives undefined', () => {
    expect(thumbnailSrc(undefined)).toBeUndefined();
    expect(thumbnailSrc(null)).toBeUndefined();
    expect(thumbnailSrc('')).toBeUndefined();
  });

  test('a path stays a path on the same origin', () => {
    expect(thumbnailSrc('/api/v1/contacts/5/profile_picture?thumbnail=true&v=x')).toMatch(
      /\/api\/v1\/contacts\/5\/profile_picture\?thumbnail=true&v=x$/,
    );
  });

  test('a full URL is kept', () => {
    expect(thumbnailSrc('https://example.com/a.jpg')).toBe('https://example.com/a.jpg');
  });
});
