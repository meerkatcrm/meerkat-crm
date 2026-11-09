import { test, expect, vi, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor, cleanup } from '@testing-library/react';
import { I18nextProvider } from 'react-i18next';
import i18n from '../i18n/config';
import { DateFormatProvider } from '../DateFormatProvider';
import AddEmploymentHistoryDialog from './AddEmploymentHistoryDialog';

vi.mock('../context/SnackbarContext', () => ({
  useSnackbar: () => ({ showError: vi.fn(), showSuccess: vi.fn() }),
}));

afterEach(() => {
  cleanup();
  window.localStorage.removeItem('dateFormat');
});

function renderDialog(props: Partial<React.ComponentProps<typeof AddEmploymentHistoryDialog>> = {}) {
  const p = { open: true, onClose: vi.fn(), onSave: vi.fn().mockResolvedValue(undefined), ...props };
  render(
    <I18nextProvider i18n={i18n}>
      <DateFormatProvider>
        <AddEmploymentHistoryDialog {...p} />
      </DateFormatProvider>
    </I18nextProvider>
  );
  return p;
}

const type = (label: string, value: string) =>
  fireEvent.change(screen.getByLabelText(new RegExp(label)), { target: { value } });

test('requires at least one job detail', () => {
  const p = renderDialog();
  fireEvent.click(screen.getByText('Save'));
  expect(screen.getByText('Enter an organization, job title, department or role')).toBeInTheDocument();
  expect(p.onSave).not.toHaveBeenCalled();
});

test('rejects malformed dates', () => {
  const p = renderDialog();
  type('Organization', 'Acme');
  type('Start Date', '15/03/2020');
  fireEvent.click(screen.getByText('Save'));
  expect(screen.getByText('Dates must be YYYY, MM.YYYY, or DD.MM.YYYY')).toBeInTheDocument();
  expect(p.onSave).not.toHaveBeenCalled();
});

test.each([
  ['eu', '15.03.2020', '03.2020'],
  ['us', '03/15/2020', '03/2020'],
  ['iso', '2020-03-15', '2020-03'],
])('%s: takes dates in the preferred format and stores them as ISO', async (format, full, partial) => {
  window.localStorage.setItem('dateFormat', format);
  const p = renderDialog();
  type('Organization', 'Acme');
  type('Start Date', full);
  type('End Date', partial);
  fireEvent.click(screen.getByText('Save'));
  await waitFor(() => expect(p.onClose).toHaveBeenCalled());
  expect(p.onSave).toHaveBeenCalledWith(
    expect.objectContaining({ start_date: '2020-03-15', end_date: '2020-03' })
  );
});

test.each([
  ['eu', '15.03.2020', '03.2020'],
  ['us', '03/15/2020', '03/2020'],
  ['iso', '2020-03-15', '2020-03'],
])('%s: edit mode shows stored dates in the preferred format', (format, full, partial) => {
  window.localStorage.setItem('dateFormat', format);
  renderDialog({
    entry: { ID: 1, CreatedAt: '', UpdatedAt: '', contact_id: 1, organization: 'Acme', start_date: '2020-03-15', end_date: '2020-03' },
  });
  expect(screen.getByDisplayValue(full)).toBeInTheDocument();
  expect(screen.getByDisplayValue(partial)).toBeInTheDocument();
});

test('saves trimmed data, omits blanks, then closes', async () => {
  const p = renderDialog();
  type('Organization', '  Acme ');
  type('Start Date', '2020-03');
  fireEvent.click(screen.getByText('Save'));
  await waitFor(() => expect(p.onClose).toHaveBeenCalled());
  expect(p.onSave).toHaveBeenCalledWith({
    organization: 'Acme',
    department: undefined,
    job_title: undefined,
    role: undefined,
    start_date: '2020-03',
    end_date: undefined,
    notes: undefined,
  });
});

test('edit mode prefills from the entry', () => {
  renderDialog({
    entry: { ID: 1, CreatedAt: '', UpdatedAt: '', contact_id: 1, organization: 'Acme', job_title: 'Dev', start_date: '2019' },
  });
  expect(screen.getByText('Edit Job')).toBeInTheDocument();
  expect(screen.getByDisplayValue('Acme')).toBeInTheDocument();
  expect(screen.getByDisplayValue('Dev')).toBeInTheDocument();
  expect(screen.getByDisplayValue('2019')).toBeInTheDocument();
});

test('stays open and shows the error when save fails', async () => {
  const p = renderDialog({ onSave: vi.fn().mockRejectedValue(new Error('boom')) });
  type('Organization', 'Acme');
  fireEvent.click(screen.getByText('Save'));
  await waitFor(() => expect(screen.getByText('boom')).toBeInTheDocument());
  expect(p.onClose).not.toHaveBeenCalled();
});
