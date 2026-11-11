import { test, expect, vi, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor, cleanup } from '@testing-library/react';
import { I18nextProvider } from 'react-i18next';
import i18n from '../i18n/config';
import { DateFormatProvider } from '../DateFormatProvider';
import AddRelationshipDialog from './AddRelationshipDialog';
import { Relationship } from '../api/relationships';

vi.mock('../context/SnackbarContext', () => ({
  useSnackbar: () => ({ showError: vi.fn(), showSuccess: vi.fn() }),
}));

vi.mock('../hooks/useContactSearch', () => ({
  useContactSearch: () => ({ options: [], loading: false, search: '', setSearch: vi.fn() }),
}));

afterEach(() => {
  cleanup();
  window.localStorage.removeItem('dateFormat');
});

const relationship = (over: Partial<Relationship> = {}): Relationship => ({
  ID: 7,
  CreatedAt: '',
  UpdatedAt: '',
  name: 'Sam Doe',
  type: 'Spouse',
  contact_id: 1,
  ...over,
});

function renderDialog(props: Partial<React.ComponentProps<typeof AddRelationshipDialog>> = {}) {
  const p = {
    open: true,
    onClose: vi.fn(),
    onSave: vi.fn().mockResolvedValue(undefined),
    currentContactId: 1,
    ...props,
  };
  render(
    <I18nextProvider i18n={i18n}>
      <DateFormatProvider>
        <AddRelationshipDialog {...p} />
      </DateFormatProvider>
    </I18nextProvider>
  );
  return p;
}

const type = (label: string, value: string) =>
  fireEvent.change(screen.getByLabelText(new RegExp(label)), { target: { value } });

test.each([
  ['eu', '15.03.2010', '06.2018'],
  ['us', '03/15/2010', '06/2018'],
  ['iso', '2010-03-15', '2018-06'],
])('%s: edit shows stored dates in the preferred format', (format, start, end) => {
  window.localStorage.setItem('dateFormat', format);
  renderDialog({ relationship: relationship({ start_date: '2010-03-15', end_date: '2018-06' }) });
  expect(screen.getByLabelText(/Start Date/)).toHaveValue(start);
  expect(screen.getByLabelText(/End Date/)).toHaveValue(end);
});

test.each([
  ['eu', '15.03.2010', '06.2018'],
  ['us', '03/15/2010', '06/2018'],
  ['iso', '2010-03-15', '2018-06'],
])('%s: saves dates typed in the preferred format as ISO', async (format, start, end) => {
  window.localStorage.setItem('dateFormat', format);
  const p = renderDialog({ relationship: relationship() });
  type('Start Date', start);
  type('End Date', end);
  fireEvent.click(screen.getByText('Save'));
  await waitFor(() => expect(p.onClose).toHaveBeenCalled());
  expect(p.onSave).toHaveBeenCalledWith(
    expect.objectContaining({ start_date: '2010-03-15', end_date: '2018-06' })
  );
});

test('an unrelated edit keeps the stored dates', async () => {
  const p = renderDialog({ relationship: relationship({ start_date: '2010', end_date: '2018-06' }) });
  type('Name', 'Samuel Doe');
  fireEvent.click(screen.getByText('Save'));
  await waitFor(() => expect(p.onClose).toHaveBeenCalled());
  expect(p.onSave).toHaveBeenCalledWith(
    expect.objectContaining({ name: 'Samuel Doe', start_date: '2010', end_date: '2018-06' })
  );
});

test('clearing the end date sends an empty string so the backend clears it', async () => {
  const p = renderDialog({ relationship: relationship({ end_date: '2018' }) });
  type('End Date', '');
  fireEvent.click(screen.getByText('Save'));
  await waitFor(() => expect(p.onClose).toHaveBeenCalled());
  expect(p.onSave).toHaveBeenCalledWith(expect.objectContaining({ end_date: '' }));
});

test('rejects an end date before the start date', () => {
  const p = renderDialog({ relationship: relationship() });
  type('Start Date', '2015');
  type('End Date', '2014-12');
  fireEvent.click(screen.getByText('Save'));
  expect(screen.getByText('End date must not be before the start date')).toBeInTheDocument();
  expect(p.onSave).not.toHaveBeenCalled();
});

test('accepts an end date in the same period as a coarser start date', async () => {
  const p = renderDialog({ relationship: relationship() });
  type('Start Date', '2015');
  type('End Date', '2015-01');
  fireEvent.click(screen.getByText('Save'));
  await waitFor(() => expect(p.onClose).toHaveBeenCalled());
});

test('rejects malformed dates', () => {
  window.localStorage.setItem('dateFormat', 'eu');
  const p = renderDialog({ relationship: relationship() });
  type('End Date', '2018/06');
  fireEvent.click(screen.getByText('Save'));
  expect(screen.getByText('Dates must be YYYY, MM.YYYY, or DD.MM.YYYY')).toBeInTheDocument();
  expect(p.onSave).not.toHaveBeenCalled();
});
