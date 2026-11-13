import { test, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, cleanup, within, fireEvent } from '@testing-library/react';
import { MemoryRouter, Routes, Route, Link } from 'react-router-dom';
import './i18n/config';
import ContactsPage from './ContactsPage';
import { useContacts } from './hooks/useContacts';

vi.mock('./hooks/useContacts', () => ({
  useContacts: vi.fn(() => ({ contacts: [], total: 0, loading: false, error: null, refetch: vi.fn() })),
}));
vi.mock('./api/contacts', () => ({ getCircles: vi.fn(async () => []) }));
vi.mock('./api/admin', () => ({ getCurrentUser: vi.fn(async () => ({})) }));
vi.mock('./components/AddContactDialog', () => ({ default: () => null }));
vi.mock('./components/ImportContactsDialog', () => ({ default: () => null }));

const lastParams = () => vi.mocked(useContacts).mock.calls.at(-1)![0];

function renderAt(url: string) {
  render(
    <MemoryRouter initialEntries={[url]}>
      <ContactsPage />
    </MemoryRouter>,
  );
}

beforeEach(() => localStorage.setItem('contacts-sort-option', 'firstname-asc'));
afterEach(() => {
  cleanup();
  localStorage.clear();
  vi.mocked(useContacts).mockClear();
});

test('without a search the remembered sort is used and Best match is not offered', () => {
  renderAt('/contacts');
  expect(lastParams()).toMatchObject({ sort: 'firstname', order: 'asc' });
  fireEvent.mouseDown(screen.getByRole('combobox', { name: 'Sort by' }));
  expect(within(screen.getByRole('listbox')).queryByText('Best match')).not.toBeInTheDocument();
});

test('a search asks the server for its ranking, and another order can still be picked', () => {
  renderAt('/contacts?search=zoe');
  expect(screen.getByRole('combobox', { name: 'Sort by' })).toHaveTextContent('Best match');
  expect(lastParams()).toMatchObject({ search: 'zoe', sort: undefined, order: undefined });

  fireEvent.mouseDown(screen.getByRole('combobox', { name: 'Sort by' }));
  fireEvent.click(within(screen.getByRole('listbox')).getByText('Name Z-A'));
  expect(lastParams()).toMatchObject({ sort: 'firstname', order: 'desc' });
  expect(localStorage.getItem('contacts-sort-option')).toBe('firstname-asc');
});

test('a new search starts at Best match again', () => {
  render(
    <MemoryRouter initialEntries={['/contacts?search=zoe']}>
      <Routes>
        <Route
          path="/contacts"
          element={
            <>
              <ContactsPage />
              <Link to="/contacts?search=ann">next</Link>
              <Link to="/contacts?search=zoe">back</Link>
            </>
          }
        />
      </Routes>
    </MemoryRouter>,
  );
  fireEvent.mouseDown(screen.getByRole('combobox', { name: 'Sort by' }));
  fireEvent.click(within(screen.getByRole('listbox')).getByText('Name Z-A'));
  expect(lastParams()).toMatchObject({ sort: 'firstname', order: 'desc' });

  fireEvent.click(screen.getByText('next'));
  expect(lastParams()).toMatchObject({ search: 'ann', sort: undefined, order: undefined });
  expect(screen.getByRole('combobox', { name: 'Sort by' })).toHaveTextContent('Best match');

  fireEvent.click(screen.getByText('back'));
  expect(lastParams()).toMatchObject({ search: 'zoe', sort: undefined, order: undefined });
});
