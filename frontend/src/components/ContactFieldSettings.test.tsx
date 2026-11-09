import { test, expect, vi, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor, cleanup } from '@testing-library/react';
import { I18nextProvider } from 'react-i18next';
import i18n from '../i18n/config';
import ContactFieldSettings from './ContactFieldSettings';
import { updateEnabledContactFields } from '../api/users';

vi.mock('../api/users', () => ({
  updateEnabledContactFields: vi.fn(),
  updateContactFieldDisplay: vi.fn(),
}));
vi.mock('../api/admin', () => ({
  getCurrentUser: vi.fn().mockResolvedValue({ enabled_contact_fields: ['emails'] }),
}));

afterEach(() => {
  cleanup();
  vi.mocked(updateEnabledContactFields).mockReset();
});

async function renderEmailSwitch() {
  render(
    <I18nextProvider i18n={i18n}>
      <ContactFieldSettings />
    </I18nextProvider>
  );
  const emailSwitch = await screen.findByRole('switch', { name: 'Email' });
  expect(emailSwitch).toBeChecked();
  return emailSwitch;
}

test('a field switch keeps its new position when the save succeeds', async () => {
  vi.mocked(updateEnabledContactFields).mockResolvedValue([]);
  const emailSwitch = await renderEmailSwitch();

  fireEvent.click(emailSwitch);

  await waitFor(() => expect(updateEnabledContactFields).toHaveBeenCalledWith([]));
  await waitFor(() => expect(emailSwitch).toBeEnabled());
  expect(emailSwitch).not.toBeChecked();
});

test('a field switch goes back and shows the error when the save fails', async () => {
  vi.mocked(updateEnabledContactFields).mockRejectedValue(new Error('boom'));
  const emailSwitch = await renderEmailSwitch();

  fireEvent.click(emailSwitch);

  expect(await screen.findByText('boom')).toBeInTheDocument();
  expect(emailSwitch).toBeChecked();
});
