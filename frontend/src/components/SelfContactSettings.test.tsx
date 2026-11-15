import { describe, test, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import i18n from '../i18n/config';
import { SnackbarProvider } from '../context/SnackbarContext';
import SelfContactSettings from './SelfContactSettings';
import { getCurrentUser } from '../api/admin';
import { getContact, getContacts } from '../api/contacts';
import { setSelfContact } from '../api/users';

vi.mock('../api/admin', () => ({ getCurrentUser: vi.fn() }));
vi.mock('../api/users', () => ({ setSelfContact: vi.fn() }));
vi.mock('../api/contacts', () => ({ getContact: vi.fn(), getContacts: vi.fn() }));

const ada = { ID: 1, firstname: 'Ada', lastname: 'Lovelace' };
const bob = { ID: 2, firstname: 'Bob', lastname: 'Builder' };

function renderSettings() {
  render(
    <SnackbarProvider>
      <SelfContactSettings />
    </SnackbarProvider>
  );
  return screen.getByRole('combobox', { name: 'Contact that represents you' });
}

describe('SelfContactSettings', () => {
  beforeEach(async () => {
    vi.resetAllMocks();
    await i18n.changeLanguage('en');
    vi.mocked(getContacts).mockResolvedValue({ contacts: [ada, bob], total: 2, page: 1, limit: 100 } as never);
  });

  test('shows the contact currently marked as you', async () => {
    vi.mocked(getCurrentUser).mockResolvedValue({ self_contact_id: 1 } as never);
    vi.mocked(getContact).mockResolvedValue(ada as never);
    const input = renderSettings();
    await waitFor(() => expect(input).toHaveValue('Ada Lovelace'));
    expect(getContact).toHaveBeenCalledWith(1, expect.arrayContaining(['firstname', 'lastname']));
  });

  test('choosing a contact marks it', async () => {
    vi.mocked(getCurrentUser).mockResolvedValue({ self_contact_id: null } as never);
    vi.mocked(setSelfContact).mockResolvedValue(2);
    const input = renderSettings();
    await waitFor(() => expect(input).toBeEnabled());
    expect(getContact).not.toHaveBeenCalled();

    fireEvent.mouseDown(input);
    fireEvent.click(await screen.findByRole('option', { name: 'Bob Builder' }));

    await waitFor(() => expect(setSelfContact).toHaveBeenCalledWith(2));
    expect(input).toHaveValue('Bob Builder');
    expect(await screen.findByText('Contact marked as you')).toBeInTheDocument();
  });

  test('clearing the field removes the mark', async () => {
    vi.mocked(getCurrentUser).mockResolvedValue({ self_contact_id: 1 } as never);
    vi.mocked(getContact).mockResolvedValue(ada as never);
    vi.mocked(setSelfContact).mockResolvedValue(null);
    const input = renderSettings();
    await waitFor(() => expect(input).toHaveValue('Ada Lovelace'));

    // MUI hides the clear button until hover, so it has no accessible name to query by in jsdom.
    fireEvent.click(screen.getByTitle('Clear'));

    await waitFor(() => expect(setSelfContact).toHaveBeenCalledWith(null));
    expect(input).toHaveValue('');
  });

  test('a failed save puts the previous contact back', async () => {
    vi.mocked(getCurrentUser).mockResolvedValue({ self_contact_id: 1 } as never);
    vi.mocked(getContact).mockResolvedValue(ada as never);
    vi.mocked(setSelfContact).mockRejectedValue(new Error('boom'));
    const input = renderSettings();
    await waitFor(() => expect(input).toHaveValue('Ada Lovelace'));

    fireEvent.mouseDown(input);
    fireEvent.click(await screen.findByRole('option', { name: 'Bob Builder' }));

    expect(await screen.findByText('Unable to update which contact is you')).toBeInTheDocument();
    await waitFor(() => expect(input).toHaveValue('Ada Lovelace'));
  });

  test('stays usable when the marked contact itself cannot be read', async () => {
    vi.mocked(getCurrentUser).mockResolvedValue({ self_contact_id: 1 } as never);
    vi.mocked(getContact).mockRejectedValue(new Error('not found'));
    const input = renderSettings();
    await waitFor(() => expect(input).toBeEnabled());
    expect(input).toHaveValue('');
  });

  test('stays disabled when the current mark cannot be loaded', async () => {
    vi.mocked(getCurrentUser).mockRejectedValue(new Error('offline'));
    const input = renderSettings();
    await waitFor(() => expect(getCurrentUser).toHaveBeenCalled());
    expect(input).toBeDisabled();
  });
});
