import { test, expect, vi, afterEach } from 'vitest';
import { render, screen, cleanup, fireEvent, waitFor, act } from '@testing-library/react';
import { MemoryRouter, Routes, Route, Link, useLocation } from 'react-router-dom';
import '../i18n/config';
import ContactListNavigator from './ContactListNavigator';
import { SnackbarProvider } from '../context/SnackbarContext';
import { getContacts } from '../api/contacts';

vi.mock('../api/contacts', () => ({ getContacts: vi.fn() }));

afterEach(() => {
  cleanup();
  vi.mocked(getContacts).mockReset();
});

function Where() {
  const location = useLocation();
  return <div data-testid="where">{location.pathname} {JSON.stringify(location.state)}</div>;
}

function renderWith(state: unknown) {
  render(
    <SnackbarProvider>
      <MemoryRouter initialEntries={[{ pathname: '/contacts/5', state }]}>
        <Routes>
          <Route path="/contacts/:id" element={<><ContactListNavigator /><Where /></>} />
        </Routes>
      </MemoryRouter>
    </SnackbarProvider>,
  );
}

const params = { search: 'ann', sort: 'firstname', order: 'asc' };

test('renders nothing when the contact was not opened from the list', () => {
  renderWith(null);
  expect(screen.queryByRole('button', { name: 'Next' })).not.toBeInTheDocument();
});

test('the first contact has no previous one', () => {
  renderWith({ listNav: { params, index: 0, total: 3 } });
  expect(screen.getByText('1 of 3')).toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Previous' })).toBeDisabled();
  expect(screen.getByRole('button', { name: 'Next' })).toBeEnabled();
});

test('next loads the contact at the following position of the same list', async () => {
  vi.mocked(getContacts).mockResolvedValue({ contacts: [{ ID: 9, firstname: 'Bo', lastname: '' }], total: 4, page: 3, limit: 1 });
  renderWith({ listNav: { params, index: 1, total: 3 } });

  fireEvent.click(screen.getByRole('button', { name: 'Next' }));
  await waitFor(() => expect(screen.getByTestId('where')).toHaveTextContent('/contacts/9'));
  expect(getContacts).toHaveBeenCalledWith({ ...params, page: 3, limit: 1 });
  // The total follows the list as it is now
  expect(screen.getByText('3 of 4')).toBeInTheDocument();
});

test('the last contact has no next one', () => {
  renderWith({ listNav: { params, index: 2, total: 3 } });
  expect(screen.getByRole('button', { name: 'Next' })).toBeDisabled();
});

test('a step that resolves after the user left does not pull them back', async () => {
  let resolve!: (v: unknown) => void;
  vi.mocked(getContacts).mockReturnValue(new Promise((r) => { resolve = r; }) as never);
  render(
    <SnackbarProvider>
      <MemoryRouter initialEntries={[{ pathname: '/contacts/5', state: { listNav: { params, index: 0, total: 3 } } }]}>
        <Routes>
          <Route path="/contacts/:id" element={<><ContactListNavigator /><Link to="/activities">leave</Link></>} />
          <Route path="/activities" element={<Where />} />
        </Routes>
      </MemoryRouter>
    </SnackbarProvider>,
  );
  fireEvent.click(screen.getByRole('button', { name: 'Next' }));
  fireEvent.click(screen.getByText('leave'));
  await act(async () => resolve({ contacts: [{ ID: 9, firstname: 'Bo', lastname: '' }], total: 3, page: 2, limit: 1 }));
  expect(screen.getByTestId('where')).toHaveTextContent('/activities');
});

test('a list that shrank under the contact drops the buttons instead of dead-ending', async () => {
  vi.mocked(getContacts).mockResolvedValue({ contacts: [], total: 2, page: 4, limit: 1 });
  renderWith({ listNav: { params, index: 4, total: 5 } });

  fireEvent.click(screen.getByRole('button', { name: 'Previous' }));
  expect(await screen.findByText('That contact is no longer in the list')).toBeInTheDocument();
  await waitFor(() => expect(screen.queryByRole('button', { name: 'Previous' })).not.toBeInTheDocument());
  expect(screen.getByTestId('where')).toHaveTextContent('/contacts/5 null');
});
