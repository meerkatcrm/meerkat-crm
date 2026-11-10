import { test, expect, vi, afterEach } from 'vitest';
import { render, screen, fireEvent, cleanup, waitFor, within } from '@testing-library/react';
import { I18nextProvider } from 'react-i18next';
import i18n from '../i18n/config';
import EmploymentHistoryList from './EmploymentHistoryList';
import { DateFormatProvider } from '../DateFormatProvider';
import { EmploymentHistoryEntry } from '../api/employmentHistory';

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

const entry = (over: Partial<EmploymentHistoryEntry> = {}): EmploymentHistoryEntry => ({
  ID: 1,
  CreatedAt: '',
  UpdatedAt: '',
  contact_id: 1,
  organization: 'Acme',
  ...over,
});

function renderList(entries: EmploymentHistoryEntry[], handlers = {}) {
  const props = { onEdit: vi.fn(), onDelete: vi.fn(), onEnd: vi.fn(), ...handlers };
  render(
    <I18nextProvider i18n={i18n}>
      <DateFormatProvider>
        <EmploymentHistoryList entries={entries} {...props} />
      </DateFormatProvider>
    </I18nextProvider>
  );
  return props;
}

test('shows empty state', () => {
  renderList([]);
  expect(screen.getByText('No employment history yet')).toBeInTheDocument();
});

test('open-ended entry shows Current chip, Present, and End button', () => {
  renderList([entry({ start_date: '2020-03-15', job_title: 'Dev', department: 'R&D' })]);
  expect(screen.getByText('Current')).toBeInTheDocument();
  expect(screen.getByText('Dev · R&D')).toBeInTheDocument();
  expect(screen.getByText('15.03.2020 – Present')).toBeInTheDocument();
  expect(screen.getByLabelText('End this job')).toBeInTheDocument();
});

test('partial dates keep their precision in the user date format', () => {
  renderList([entry({ start_date: '2018', end_date: '2020-06' })]);
  expect(screen.getByText('2018 – 06.2020')).toBeInTheDocument();
  expect(screen.queryByText('Current')).not.toBeInTheDocument();
  expect(screen.queryByLabelText('End this job')).not.toBeInTheDocument();
});

test('missing start date shows Unknown', () => {
  renderList([entry()]);
  expect(screen.getByText('Unknown – Present')).toBeInTheDocument();
});

test('delete and end only fire after confirmation', async () => {
  const h = renderList([entry({ ID: 7 })]);
  fireEvent.click(screen.getByLabelText('Delete'));
  expect(screen.getByText('Delete this employment history entry?')).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
  await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
  expect(h.onDelete).not.toHaveBeenCalled();

  fireEvent.click(screen.getByLabelText('Delete'));
  fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Delete' }));
  await waitFor(() => expect(h.onDelete).toHaveBeenCalledWith(7));
  await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());

  fireEvent.click(screen.getByLabelText('End this job'));
  expect(h.onEnd).not.toHaveBeenCalled();
  fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'End this job' }));
  await waitFor(() => expect(h.onEnd).toHaveBeenCalledWith(7));
});

test('edit passes the entry', () => {
  const e = entry({ ID: 3 });
  const h = renderList([e]);
  fireEvent.click(screen.getByLabelText('Edit'));
  expect(h.onEdit).toHaveBeenCalledWith(e);
});
