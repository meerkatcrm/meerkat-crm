import { test, expect } from 'vitest';
import { CONTACT_FIELDS, DEFAULT_ENABLED_CONTACT_FIELDS, resolveEnabledFields } from './contactFields';

test('every field is on by default except role and work information', () => {
  const off = CONTACT_FIELDS.map((field) => field.key).filter(
    (key) => !DEFAULT_ENABLED_CONTACT_FIELDS.includes(key)
  );
  expect(off).toEqual(['role', 'work_information']);
});

test('only an unset selection gets the defaults', () => {
  expect(resolveEnabledFields(null)).toEqual(new Set(DEFAULT_ENABLED_CONTACT_FIELDS));
  expect(resolveEnabledFields(undefined)).toEqual(new Set(DEFAULT_ENABLED_CONTACT_FIELDS));
  expect(resolveEnabledFields([])).toEqual(new Set());
  expect(resolveEnabledFields(['role'])).toEqual(new Set(['role']));
});
