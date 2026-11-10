import { test, expect } from 'vitest';
import { contactDisplayName } from './contactName';

const anna = { prefix: 'Dr.', firstname: 'Anna', middle_name: 'Maria', lastname: 'Schmidt', suffix: 'PhD', nickname: 'Annie' };

test('joins first and last name', () => {
  expect(contactDisplayName(anna)).toBe('Anna Schmidt');
});

test('leaves no trailing or double space when a part is missing', () => {
  expect(contactDisplayName({ firstname: 'Anna', lastname: '' })).toBe('Anna');
  expect(contactDisplayName({ firstname: 'Anna' })).toBe('Anna');
  expect(contactDisplayName({ firstname: '', lastname: 'Schmidt' })).toBe('Schmidt');
  expect(contactDisplayName({ firstname: ' Anna ', lastname: '  ' }, { nickname: true })).toBe('Anna');
  expect(contactDisplayName({})).toBe('');
});

test('nickname goes in quotes after the first name, only when asked for', () => {
  expect(contactDisplayName(anna, { nickname: true })).toBe('Anna "Annie" Schmidt');
  expect(contactDisplayName({ firstname: 'Anna', nickname: 'Annie' }, { nickname: true })).toBe('Anna "Annie"');
  expect(contactDisplayName({ firstname: 'Anna', lastname: 'Schmidt', nickname: '' }, { nickname: true })).toBe('Anna Schmidt');
});

test('full adds prefix, middle name and suffix', () => {
  expect(contactDisplayName(anna, { full: true })).toBe('Dr. Anna Maria Schmidt PhD');
  expect(contactDisplayName(anna, { full: true, nickname: true })).toBe('Dr. Anna "Annie" Maria Schmidt PhD');
});
