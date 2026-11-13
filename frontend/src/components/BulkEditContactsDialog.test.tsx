import { test, expect, vi, afterEach } from 'vitest';
import { render, screen, cleanup, fireEvent, waitFor, within } from '@testing-library/react';
import '../i18n/config';
import BulkEditContactsDialog from './BulkEditContactsDialog';
import { bulkUpdateContacts } from '../api/contacts';
import { ContactFieldKey } from '../contactFields';

vi.mock('../api/contacts', () => ({ bulkUpdateContacts: vi.fn() }));

afterEach(() => {
  cleanup();
  vi.mocked(bulkUpdateContacts).mockReset();
});

function renderDialog(enabled: ContactFieldKey[] = ['addresses', 'organization', 'food_preference']) {
  const onComplete = vi.fn();
  render(
    <BulkEditContactsDialog
      open
      contactIds={[4, 7]}
      availableCircles={['friends', 'work']}
      enabledFields={new Set(enabled)}
      onClose={vi.fn()}
      onComplete={onComplete}
    />,
  );
  return { onComplete };
}

const applyButton = () => screen.getByRole('button', { name: 'Apply to 2 contacts' });

test('nothing can be applied until something is changed', () => {
  renderDialog();
  expect(applyButton()).toBeDisabled();
  fireEvent.change(screen.getByLabelText('Food Preferences'), { target: { value: '   ' } });
  expect(applyButton()).toBeDisabled();
});

test('shows only the fields enabled in the settings', () => {
  renderDialog(['organization']);
  expect(screen.getByLabelText('Organization')).toBeInTheDocument();
  expect(screen.queryByLabelText('City')).not.toBeInTheDocument();
  expect(screen.queryByLabelText('Food Preferences')).not.toBeInTheDocument();
});

test('sends only what was filled in, a typed circle included', async () => {
  vi.mocked(bulkUpdateContacts).mockResolvedValue({ updated: 2 });
  const { onComplete } = renderDialog();

  // Typed but not confirmed with Enter
  fireEvent.change(screen.getByLabelText('Add to circles'), { target: { value: ' gym ' } });
  fireEvent.change(screen.getByLabelText('City'), { target: { value: ' Hamburg ' } });
  fireEvent.mouseDown(screen.getByLabelText('Archive'));
  fireEvent.click(within(screen.getByRole('listbox')).getByText('Archive'));
  expect(screen.getByText(/also deletes the reminders/)).toBeInTheDocument();

  fireEvent.click(applyButton());
  await waitFor(() => expect(onComplete).toHaveBeenCalledWith(2));
  expect(bulkUpdateContacts).toHaveBeenCalledWith({
    contact_ids: [4, 7],
    add_circles: ['gym'],
    city: 'Hamburg',
    archived: true,
  });
});

test('a circle both added and removed blocks applying', () => {
  renderDialog();
  fireEvent.mouseDown(screen.getByLabelText('Remove from circles'));
  fireEvent.click(within(screen.getByRole('listbox')).getByText('work'));
  fireEvent.change(screen.getByLabelText('Add to circles'), { target: { value: 'work' } });
  expect(screen.getByText(/can't be added and removed at once: work/)).toBeInTheDocument();
  expect(applyButton()).toBeDisabled();
});

test('an error from the server is shown and the dialog stays open', async () => {
  vi.mocked(bulkUpdateContacts).mockRejectedValue(new Error('Contact not found'));
  const { onComplete } = renderDialog();
  fireEvent.change(screen.getByLabelText('Organization'), { target: { value: 'Acme' } });
  fireEvent.click(applyButton());
  expect(await screen.findByText('Contact not found')).toBeInTheDocument();
  expect(onComplete).not.toHaveBeenCalled();
});

test('circle names differing only in case are one circle', async () => {
  vi.mocked(bulkUpdateContacts).mockResolvedValue({ updated: 2 });
  const { onComplete } = renderDialog();
  fireEvent.mouseDown(screen.getByLabelText('Remove from circles'));
  fireEvent.click(within(screen.getByRole('listbox')).getByText('work'));
  fireEvent.change(screen.getByLabelText('Add to circles'), { target: { value: 'WORK' } });
  expect(screen.getByText(/can't be added and removed at once: WORK/)).toBeInTheDocument();
  expect(applyButton()).toBeDisabled();
  expect(onComplete).not.toHaveBeenCalled();
});
