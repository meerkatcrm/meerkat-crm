import { test, expect, vi, beforeEach, afterEach } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { I18nextProvider } from 'react-i18next';
import i18n from '../i18n/config';
import { DateFormatProvider } from '../DateFormatProvider';
import EditTimelineItemDialog, { TimelineItemValues } from './EditTimelineItemDialog';
import * as contactsApi from '../api/contacts';

vi.mock('../api/contacts');
const api = vi.mocked(contactsApi);

const anna = { ID: 1, firstname: 'Anna', lastname: 'Schmidt' } as contactsApi.Contact;
const ben = { ID: 2, firstname: 'Ben', lastname: '' } as contactsApi.Contact;

beforeEach(() => {
  vi.resetAllMocks();
  i18n.changeLanguage('en');
});
afterEach(cleanup);

function renderDialog(props: Partial<React.ComponentProps<typeof EditTimelineItemDialog>> = {}, values: TimelineItemValues = {}) {
  const handlers = { onClose: vi.fn(), onSave: vi.fn(), onDelete: vi.fn(), onChange: vi.fn() };
  render(
    <I18nextProvider i18n={i18n}>
      <DateFormatProvider>
        <EditTimelineItemDialog open type="activity" values={values} {...handlers} {...props} />
      </DateFormatProvider>
    </I18nextProvider>
  );
  return handlers;
}

test('the participant picker searches server-side and keeps selected contacts listed', async () => {
  // The search result does not contain Anna, who is already a participant.
  api.getContacts.mockResolvedValue({ contacts: [ben], total: 1, page: 1, limit: 100 });
  const h = renderDialog({}, { activityTitle: 'Lunch', activityContacts: [anna] });

  await waitFor(() => expect(api.getContacts).toHaveBeenCalledWith({ limit: 100, search: '', includeArchived: undefined }));
  expect(screen.getByText('Anna Schmidt')).toBeInTheDocument();

  const input = screen.getByRole('combobox');
  // Unfocused, the Autocomplete resets its input text on every render.
  fireEvent.focus(input);
  fireEvent.change(input, { target: { value: 'be' } });
  await waitFor(() => expect(api.getContacts).toHaveBeenLastCalledWith({ limit: 100, search: 'be', includeArchived: undefined }));

  const listbox = await screen.findByRole('listbox');
  expect(within(listbox).getByText('Anna Schmidt')).toBeInTheDocument();
  // No trailing space for a contact without a last name
  fireEvent.click(within(listbox).getByText('Ben'));
  expect(h.onChange).toHaveBeenCalledWith(expect.objectContaining({ activityContacts: [anna, ben] }));
});

test('a note edit does not query contacts', () => {
  renderDialog({ type: 'note' }, { noteContent: 'Hi' });
  expect(api.getContacts).not.toHaveBeenCalled();
});

test('delete asks for confirmation first', async () => {
  const h = renderDialog({ type: 'note' }, { noteContent: 'Hi' });
  fireEvent.click(screen.getByTitle('Delete'));
  expect(h.onDelete).not.toHaveBeenCalled();
  const confirm = screen.getByText('Are you sure you want to delete this item?').closest('[role="dialog"]') as HTMLElement;
  fireEvent.click(within(confirm).getByRole('button', { name: 'Delete' }));
  await waitFor(() => expect(h.onDelete).toHaveBeenCalledTimes(1));
});
