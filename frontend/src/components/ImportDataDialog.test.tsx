import { test, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor, cleanup, within } from '@testing-library/react';
import { act } from 'react';
import { I18nextProvider } from 'react-i18next';
import i18n from '../i18n/config';
import ImportDataDialog from './ImportDataDialog';
import * as api from '../api/import';
import type { DataImportResult } from '../api/import';

vi.mock('../api/import', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api/import')>()),
  importDataExport: vi.fn(),
}));

vi.setConfig({ testTimeout: 30000 });

const mocked = vi.mocked(api);
const none = { created: 0, existing: 0, skipped: 0 };

function result(overrides: Partial<DataImportResult> = {}): DataImportResult {
  return {
    dry_run: true,
    contacts: { created: 2, existing: 1, skipped: 0 },
    relationships: none,
    activities: { created: 3, existing: 0, skipped: 0 },
    notes: none,
    reminders: none,
    custom_fields_added: [],
    warnings: [],
    more_warnings: 0,
    ...overrides,
  };
}

function choose(name: string) {
  const file = new File(['x'], name);
  fireEvent.change(screen.getByTestId('data-import-file'), { target: { files: [file] } });
  return file;
}

function renderDialog(onImportComplete?: (result: DataImportResult) => void) {
  const onClose = vi.fn();
  render(
    <I18nextProvider i18n={i18n}>
      <ImportDataDialog open onClose={onClose} onImportComplete={onImportComplete} />
    </I18nextProvider>
  );
  return onClose;
}

beforeEach(async () => {
  vi.clearAllMocks();
  await act(() => i18n.changeLanguage('en'));
});

afterEach(cleanup);

test('checks the file first, then imports it on confirmation', async () => {
  mocked.importDataExport.mockResolvedValueOnce(result({ custom_fields_added: ['Pet name'] }));
  renderDialog();

  const file = choose('meerkat-export.csv');
  expect(mocked.importDataExport).toHaveBeenCalledWith(file, true);

  await screen.findByText('This is what the import will do:');
  const contactsRow = screen.getByRole('row', { name: /Contacts/ });
  expect(within(contactsRow).getAllByRole('cell').map((c) => c.textContent)).toEqual(['2', '1', '0']);
  expect(screen.getByText('New custom fields: Pet name')).toBeInTheDocument();

  mocked.importDataExport.mockResolvedValueOnce(result({ dry_run: false }));
  fireEvent.click(screen.getByRole('button', { name: 'Import' }));

  await screen.findByText('Import complete.');
  expect(mocked.importDataExport).toHaveBeenLastCalledWith(file, false);
  expect(screen.queryByRole('button', { name: 'Import' })).not.toBeInTheDocument();
  expect(screen.getByRole('columnheader', { name: 'Added' })).toBeInTheDocument();
});

test('choosing another file drops the old preview until the new one arrives', async () => {
  mocked.importDataExport.mockResolvedValueOnce(result());
  renderDialog();
  choose('first.csv');
  await screen.findByText('This is what the import will do:');
  expect(screen.getByRole('button', { name: 'Import' })).toBeEnabled();

  let finish: (value: DataImportResult) => void = () => undefined;
  mocked.importDataExport.mockReturnValueOnce(new Promise((resolve) => (finish = resolve)));
  const second = choose('second.csv');

  expect(screen.queryByText('This is what the import will do:')).not.toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Import' })).toBeDisabled();
  expect(screen.getByRole('button', { name: 'Cancel' })).toBeDisabled();

  await act(async () => finish(result({ contacts: { created: 7, existing: 0, skipped: 0 } })));
  const contactsRow = screen.getByRole('row', { name: /Contacts/ });
  expect(within(contactsRow).getAllByRole('cell')[0]).toHaveTextContent('7');

  mocked.importDataExport.mockResolvedValueOnce(result({ dry_run: false }));
  fireEvent.click(screen.getByRole('button', { name: 'Import' }));
  await screen.findByText('Import complete.');
  expect(mocked.importDataExport).toHaveBeenLastCalledWith(second, false);
});

test('a failed check of a second file leaves nothing to import', async () => {
  mocked.importDataExport.mockResolvedValueOnce(result());
  renderDialog();
  choose('first.csv');
  await screen.findByText('This is what the import will do:');

  mocked.importDataExport.mockRejectedValueOnce(new Error('this file is not a Meerkat data export'));
  choose('second.csv');
  await screen.findByText('this file is not a Meerkat data export');
  expect(screen.queryByText('This is what the import will do:')).not.toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Import' })).toBeDisabled();
});

test('reports a finished import, but not the check, to the page', async () => {
  const onImportComplete = vi.fn();
  mocked.importDataExport.mockResolvedValueOnce(result({ custom_fields_added: ['Pet name'] }));
  renderDialog(onImportComplete);
  choose('meerkat-export.csv');
  await screen.findByText('This is what the import will do:');
  expect(onImportComplete).not.toHaveBeenCalled();

  const done = result({ dry_run: false, custom_fields_added: ['Pet name'] });
  mocked.importDataExport.mockResolvedValueOnce(done);
  fireEvent.click(screen.getByRole('button', { name: 'Import' }));
  await screen.findByText('Import complete.');
  expect(onImportComplete).toHaveBeenCalledExactlyOnceWith(done);
});

test('does not offer an import when everything is already there', async () => {
  mocked.importDataExport.mockResolvedValueOnce(
    result({ contacts: { created: 0, existing: 3, skipped: 0 }, activities: { created: 0, existing: 2, skipped: 0 } })
  );
  renderDialog();
  choose('meerkat-export.csv');

  await screen.findByText('Everything in this file is already in your account.');
  expect(screen.getByRole('button', { name: 'Import' })).toBeDisabled();
});

test('says nothing can be imported when every record was skipped', async () => {
  mocked.importDataExport.mockResolvedValueOnce(
    result({
      contacts: { created: 0, existing: 0, skipped: 2 },
      activities: none,
      warnings: ['Contact "A" (row 1) was skipped', 'Contact "B" (row 2) was skipped'],
    })
  );
  renderDialog();
  choose('meerkat-export.csv');

  await screen.findByText('Nothing in this file can be imported. The warnings below explain why.');
  expect(screen.queryByText('Everything in this file is already in your account.')).not.toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Import' })).toBeDisabled();
});

test('shows warnings and how many more there are', async () => {
  mocked.importDataExport.mockResolvedValueOnce(result({ warnings: ['Note (row 3) was skipped'], more_warnings: 4 }));
  renderDialog();
  choose('meerkat-export.csv');

  await screen.findByText('Warnings: 5');
  expect(screen.getByText('Note (row 3) was skipped')).toBeInTheDocument();
  expect(screen.getByText('…and 4 more')).toBeInTheDocument();
});

test('rejects a file that is not a CSV without calling the server', () => {
  renderDialog();
  choose('contacts.vcf');

  expect(screen.getByText('Please choose a CSV file.')).toBeInTheDocument();
  expect(mocked.importDataExport).not.toHaveBeenCalled();
});

test('shows the server error when the file is not an export', async () => {
  mocked.importDataExport.mockRejectedValueOnce(new Error('this file is not a Meerkat data export'));
  renderDialog();
  choose('contacts.csv');

  await screen.findByText('this file is not a Meerkat data export');
  await waitFor(() => expect(screen.getByRole('button', { name: 'Import' })).toBeDisabled());
});
