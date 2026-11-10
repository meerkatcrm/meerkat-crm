import { test, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, act, waitFor } from '@testing-library/react';
import { useContactSearch } from './useContactSearch';
import * as contactsApi from '../api/contacts';

vi.mock('../api/contacts');
const api = vi.mocked(contactsApi);

const contact = (ID: number, firstname: string) => ({ ID, firstname, lastname: '' }) as contactsApi.Contact;
const page = (...contacts: contactsApi.Contact[]): contactsApi.ContactsResponse =>
  ({ contacts, total: contacts.length, page: 1, limit: 100 });

beforeEach(() => {
  vi.resetAllMocks();
  api.getContacts.mockResolvedValue(page(contact(1, 'Anna'), contact(2, 'Ben')));
});

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

test('does not fetch while disabled', () => {
  const { result } = renderHook(() => useContactSearch(false));
  expect(api.getContacts).not.toHaveBeenCalled();
  expect(result.current.options).toEqual([]);
  expect(result.current.loading).toBe(false);
});

test('loads the first page when enabled, within the backend page cap', async () => {
  const { result } = renderHook(() => useContactSearch(true));
  await waitFor(() => expect(result.current.options).toHaveLength(2));
  expect(api.getContacts).toHaveBeenCalledWith({ limit: 100, search: '', includeArchived: undefined });
  expect(result.current.loading).toBe(false);
});

test('passes includeArchived and leaves out the excluded contact', async () => {
  const { result } = renderHook(() => useContactSearch(true, { excludeId: 1, includeArchived: true }));
  await waitFor(() => expect(result.current.options).toEqual([contact(2, 'Ben')]));
  expect(api.getContacts).toHaveBeenCalledWith({ limit: 100, search: '', includeArchived: true });
});

test('searches server-side, debounced', async () => {
  vi.useFakeTimers();
  const { result } = renderHook(() => useContactSearch(true));
  await act(async () => { await vi.advanceTimersByTimeAsync(0); });
  expect(api.getContacts).toHaveBeenCalledTimes(1);

  act(() => result.current.setSearch('a'));
  act(() => result.current.setSearch('an'));
  await act(async () => { await vi.advanceTimersByTimeAsync(299); });
  expect(api.getContacts).toHaveBeenCalledTimes(1);
  await act(async () => { await vi.advanceTimersByTimeAsync(1); });
  expect(api.getContacts).toHaveBeenCalledTimes(2);
  expect(api.getContacts).toHaveBeenLastCalledWith({ limit: 100, search: 'an', includeArchived: undefined });
});

test('a slower response for an older query does not replace a newer one', async () => {
  vi.useFakeTimers();
  let resolveFirst!: (r: contactsApi.ContactsResponse) => void;
  api.getContacts
    .mockImplementationOnce(() => new Promise((r) => { resolveFirst = r; }))
    .mockResolvedValueOnce(page(contact(3, 'Cleo')));
  const { result } = renderHook(() => useContactSearch(true));

  act(() => result.current.setSearch('c'));
  await act(async () => { await vi.advanceTimersByTimeAsync(300); });
  expect(result.current.options).toEqual([contact(3, 'Cleo')]);

  await act(async () => resolveFirst(page(contact(1, 'Anna'))));
  expect(result.current.options).toEqual([contact(3, 'Cleo')]);
});

test('disabling clears the search and the options', async () => {
  const { result, rerender } = renderHook(({ enabled }) => useContactSearch(enabled), { initialProps: { enabled: true } });
  await waitFor(() => expect(result.current.options).toHaveLength(2));
  act(() => result.current.setSearch('x'));
  rerender({ enabled: false });
  expect(result.current.options).toEqual([]);
  expect(result.current.search).toBe('');
});

test('a failed search keeps the picker usable', async () => {
  vi.spyOn(console, 'error').mockImplementation(() => {});
  api.getContacts.mockRejectedValue(new Error('offline'));
  const { result } = renderHook(() => useContactSearch(true));
  await waitFor(() => expect(api.getContacts).toHaveBeenCalled());
  await waitFor(() => expect(result.current.loading).toBe(false));
  expect(result.current.options).toEqual([]);
});
