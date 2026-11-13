import { test, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, cleanup, within, fireEvent, waitFor, act } from '@testing-library/react';
import { MemoryRouter, Routes, Route, Link, useLocation } from 'react-router-dom';
import { SnackbarProvider } from './context/SnackbarContext';
import { getContacts } from './api/contacts';
import './i18n/config';
import ContactsPage from './ContactsPage';
import { useContacts } from './hooks/useContacts';

vi.mock('./hooks/useContacts', () => ({
  useContacts: vi.fn(() => ({ contacts: [], total: 0, loading: false, error: null, refetch: vi.fn() })),
}));
vi.mock('./api/contacts', () => ({ getCircles: vi.fn(async () => []), getContacts: vi.fn() }));
vi.mock('./api/admin', () => ({ getCurrentUser: vi.fn(async () => ({})) }));
vi.mock('./components/AddContactDialog', () => ({ default: () => null }));
vi.mock('./components/ImportContactsDialog', () => ({ default: () => null }));
vi.mock('./components/BulkEditContactsDialog', () => ({
  default: ({ open, contactIds, onComplete }: { open: boolean; contactIds: number[]; onComplete: (n: number) => void }) =>
    open ? (
      <div data-testid="bulk-dialog">
        {[...contactIds].sort((a, b) => a - b).join(',')}
        <button onClick={() => onComplete(2)}>finish-2</button>
        <button onClick={() => onComplete(0)}>finish-0</button>
      </div>
    ) : null,
}));

const lastParams = () => vi.mocked(useContacts).mock.calls.at(-1)![0];

function renderAt(url: string) {
  render(
    <SnackbarProvider>
      <MemoryRouter initialEntries={[url]}>
        <ContactsPage />
      </MemoryRouter>
    </SnackbarProvider>,
  );
}

beforeEach(() => localStorage.setItem('contacts-sort-option', 'firstname-asc'));
afterEach(() => {
  cleanup();
  localStorage.clear();
  vi.mocked(useContacts).mockReset();
  vi.mocked(getContacts).mockReset();
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
    <SnackbarProvider>
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
    </MemoryRouter>
    </SnackbarProvider>,
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

const contact = (ID: number, firstname: string) => ({ ID, firstname, lastname: '', circles: [] });

function mockList(contacts: ReturnType<typeof contact>[], total = contacts.length) {
  vi.mocked(useContacts).mockReturnValue({ contacts, total, loading: false, error: null, refetch: vi.fn() } as never);
}

test('in select mode a click selects a contact instead of opening it, and the selection can be edited', () => {
  mockList([contact(1, 'Ann'), contact(2, 'Bob'), contact(3, 'Cy')]);
  renderAt('/contacts');

  fireEvent.click(screen.getByRole('button', { name: 'Select' }));
  fireEvent.click(screen.getByRole('checkbox', { name: /Ann/ }));
  fireEvent.click(screen.getByRole('checkbox', { name: /Cy/ }));
  expect(screen.getByText('2 selected')).toBeInTheDocument();
  expect(screen.queryByRole('link', { name: /Ann/ })).not.toBeInTheDocument();

  fireEvent.click(screen.getByRole('button', { name: 'Edit selected' }));
  expect(screen.getByTestId('bulk-dialog')).toHaveTextContent('1,3');
});

test('the page checkbox selects every contact on the page', () => {
  mockList([contact(1, 'Ann'), contact(2, 'Bob')]);
  renderAt('/contacts');
  fireEvent.click(screen.getByRole('button', { name: 'Select' }));
  fireEvent.click(screen.getByRole('checkbox', { name: 'Select all on this page' }));
  expect(screen.getByText('2 selected')).toBeInTheDocument();
});

test('select all collects every matching contact across pages', async () => {
  mockList([contact(1, 'Ann')], 150);
  const page = (from: number, n: number) => Array.from({ length: n }, (_, i) => contact(from + i, 'X'));
  vi.mocked(getContacts)
    .mockResolvedValueOnce({ contacts: page(1, 100), total: 150, page: 1, limit: 100 } as never)
    .mockResolvedValueOnce({ contacts: page(101, 50), total: 150, page: 2, limit: 100 } as never);
  renderAt('/contacts?search=x');

  fireEvent.click(screen.getByRole('button', { name: 'Select' }));
  fireEvent.click(screen.getByRole('button', { name: 'Select all 150' }));
  await waitFor(() => expect(screen.getByText('150 selected')).toBeInTheDocument());
  expect(vi.mocked(getContacts).mock.calls.map(([p]) => [p.page, p.limit, p.search, p.sort, p.order])).toEqual([
    [1, 100, 'x', 'id', 'desc'],
    [2, 100, 'x', 'id', 'desc'],
  ]);
});

function ShowState() {
  const location = useLocation();
  return <div data-testid="state">{JSON.stringify(location.state)}</div>;
}

test('opening a contact carries its place in the list for previous / next', () => {
  mockList([contact(1, 'Ann'), contact(2, 'Bob')], 12);
  render(
    <SnackbarProvider>
      <MemoryRouter initialEntries={['/contacts?page=2']}>
        <Routes>
          <Route path="/contacts" element={<ContactsPage />} />
          <Route path="/contacts/:id" element={<ShowState />} />
        </Routes>
      </MemoryRouter>
    </SnackbarProvider>,
  );
  fireEvent.click(screen.getByRole('link', { name: /Bob/ }));
  const state = JSON.parse(screen.getByTestId('state').textContent!);
  expect(state.listNav).toMatchObject({ index: 11, total: 12, params: { sort: 'firstname', order: 'asc' } });
  expect(state.listNav.params).not.toHaveProperty('page');
});

test('a random order gives no previous / next', () => {
  localStorage.setItem('contacts-sort-option', 'random-asc');
  mockList([contact(1, 'Ann')]);
  render(
    <SnackbarProvider>
      <MemoryRouter initialEntries={['/contacts']}>
        <Routes>
          <Route path="/contacts" element={<ContactsPage />} />
          <Route path="/contacts/:id" element={<ShowState />} />
        </Routes>
      </MemoryRouter>
    </SnackbarProvider>,
  );
  fireEvent.click(screen.getByRole('link', { name: /Ann/ }));
  expect(JSON.parse(screen.getByTestId('state').textContent!).listNav).toBeUndefined();
});

test('select all keeps nothing when the filters change while it collects', async () => {
  mockList([contact(1, 'Ann')], 5);
  let resolve!: (v: unknown) => void;
  vi.mocked(getContacts).mockReturnValue(new Promise((r) => { resolve = r; }) as never);
  renderAt('/contacts');

  fireEvent.click(screen.getByRole('button', { name: 'Select' }));
  fireEvent.click(screen.getByRole('button', { name: 'Select all 5' }));
  fireEvent.click(screen.getByRole('switch', { name: 'Show archived' }));
  await act(async () => resolve({ contacts: [1, 2, 3, 4, 5].map(id => contact(id, 'X')), total: 5, page: 1, limit: 100 }));
  expect(screen.getByText('0 selected')).toBeInTheDocument();
});

test('after a bulk edit the message says how many contacts changed, or that none needed to', async () => {
  mockList([contact(1, 'Ann'), contact(2, 'Bob')]);
  renderAt('/contacts');
  fireEvent.click(screen.getByRole('button', { name: 'Select' }));
  fireEvent.click(screen.getByRole('checkbox', { name: 'Select all on this page' }));
  fireEvent.click(screen.getByRole('button', { name: 'Edit selected' }));
  fireEvent.click(screen.getByText('finish-2'));
  expect(await screen.findByText('Updated 2 contacts')).toBeInTheDocument();

  fireEvent.click(screen.getByRole('checkbox', { name: 'Select all on this page' }));
  fireEvent.click(screen.getByRole('button', { name: 'Edit selected' }));
  fireEvent.click(screen.getByText('finish-0'));
  expect(await screen.findByText(/Nothing needed changing/)).toBeInTheDocument();
});

test('select all still applies when the page is turned while it collects', async () => {
  mockList([contact(1, 'Ann')], 15);
  let resolve!: (v: unknown) => void;
  vi.mocked(getContacts).mockReturnValue(new Promise((r) => { resolve = r; }) as never);
  renderAt('/contacts');

  fireEvent.click(screen.getByRole('button', { name: 'Select' }));
  fireEvent.click(screen.getByRole('button', { name: 'Select all 15' }));
  fireEvent.click(screen.getByRole('button', { name: 'Go to page 2' }));
  await act(async () => resolve({ contacts: Array.from({ length: 15 }, (_, i) => contact(i + 1, 'X')), total: 15, page: 1, limit: 100 }));
  expect(screen.getByText('15 selected')).toBeInTheDocument();
});

test('select all applies nothing when select mode is left while it collects', async () => {
  mockList([contact(1, 'Ann')], 5);
  let resolve!: (v: unknown) => void;
  vi.mocked(getContacts).mockReturnValue(new Promise((r) => { resolve = r; }) as never);
  renderAt('/contacts');

  fireEvent.click(screen.getByRole('button', { name: 'Select' }));
  fireEvent.click(screen.getByRole('button', { name: 'Select all 5' }));
  fireEvent.click(screen.getByRole('button', { name: 'Done' }));
  await act(async () => resolve({ contacts: [1, 2, 3, 4, 5].map(id => contact(id, 'X')), total: 5, page: 1, limit: 100 }));
  fireEvent.click(screen.getByRole('button', { name: 'Select' }));
  expect(screen.getByText('0 selected')).toBeInTheDocument();
});
