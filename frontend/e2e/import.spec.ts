import type { Page } from '@playwright/test';
import { test, expect, LOGGED_OUT, signInAsNewUser, uniqueUser } from './fixtures';
import { API_BASE_URL } from './global-setup';

// Each test gets its own user, because import looks for duplicates across all of a user's contacts.
test.use({ storageState: LOGGED_OUT });

const vcard = (phone: string) =>
  ['BEGIN:VCARD', 'VERSION:3.0', 'FN:Imported Person', 'N:Person;Imported;;;', 'EMAIL;TYPE=HOME:imported@example.com', `TEL;TYPE=CELL:${phone}`, 'END:VCARD', ''].join('\r\n');

async function openImport(page: Page) {
  await page.goto('/contacts');
  await page.getByRole('button', { name: 'Import', exact: true }).click();
  const dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible();
  return dialog;
}

async function chooseFile(page: Page, name: string, mimeType: string, content: string) {
  await page.locator('#import-file-input').setInputFiles({ name, mimeType, buffer: Buffer.from(content) });
}

async function storedContacts(page: Page) {
  return (await (await page.request.get(`${API_BASE_URL}/contacts?limit=10`)).json()).contacts;
}

test.describe('Import dialog', () => {
  test('imports a vCard, then updates that contact from a second file', async ({ page }) => {
    await signInAsNewUser(page, uniqueUser('e2e_import_vcf'));

    let dialog = await openImport(page);
    await chooseFile(page, 'contacts.vcf', 'text/vcard', vcard('+1 555-0100'));
    await expect(dialog.getByText('1 to create')).toBeVisible();
    await expect(dialog.getByRole('cell', { name: 'Imported', exact: true })).toBeVisible();
    await dialog.getByRole('button', { name: 'Import', exact: true }).click();
    await expect(dialog.getByText('Import Complete')).toBeVisible();
    await dialog.getByRole('button', { name: 'Done' }).click();
    await expect(page.getByText('Imported Person')).toBeVisible();

    dialog = await openImport(page);
    await chooseFile(page, 'contacts.vcf', 'text/vcard', vcard('+1 555-0200'));
    await expect(dialog.getByText('Duplicate', { exact: true })).toBeVisible();
    await expect(dialog.getByText('1 to update')).toBeVisible();
    await dialog.getByRole('button', { name: 'Import', exact: true }).click();
    await expect(dialog.getByText('Import Complete')).toBeVisible();
    await dialog.getByRole('button', { name: 'Done' }).click();

    const contacts = await storedContacts(page);
    expect(contacts).toHaveLength(1);
    const stored = await (await page.request.get(`${API_BASE_URL}/contacts/${contacts[0].ID}`)).json();
    expect(stored.phones.map((p: { value: string }) => p.value)).toContain('+1 555-0200');
  });

  test('a skipped duplicate changes nothing', async ({ page }) => {
    await signInAsNewUser(page, uniqueUser('e2e_import_skip'));

    let dialog = await openImport(page);
    await chooseFile(page, 'contacts.vcf', 'text/vcard', vcard('+1 555-0100'));
    await dialog.getByRole('button', { name: 'Import', exact: true }).click();
    await dialog.getByRole('button', { name: 'Done' }).click();

    dialog = await openImport(page);
    await chooseFile(page, 'contacts.vcf', 'text/vcard', vcard('+1 555-0200'));
    await dialog.getByRole('row').filter({ hasText: 'Imported' }).getByRole('combobox').click();
    await page.getByRole('option', { name: 'Skip' }).click();
    await expect(dialog.getByText('1 to skip')).toBeVisible();
    await dialog.getByRole('button', { name: 'Import', exact: true }).click();
    await expect(dialog.getByText('1 rows skipped')).toBeVisible();
    await dialog.getByRole('button', { name: 'Done' }).click();

    const contacts = await storedContacts(page);
    expect(contacts).toHaveLength(1);
    const stored = await (await page.request.get(`${API_BASE_URL}/contacts/${contacts[0].ID}`)).json();
    expect(stored.phones.map((p: { value: string }) => p.value)).toEqual(['+1 555-0100']);
  });

  test('imports a CSV through the column mapping step', async ({ page }) => {
    await signInAsNewUser(page, uniqueUser('e2e_import_csv'));

    const dialog = await openImport(page);
    await chooseFile(page, 'contacts.csv', 'text/csv', 'First Name,Last Name,Email,Ignored\nCsv,Person,csv@example.com,nothing\n');
    await expect(dialog.getByRole('columnheader', { name: 'CSV Column' })).toBeVisible();
    await dialog.getByRole('button', { name: 'Continue' }).click();

    await expect(dialog.getByText('1 to create')).toBeVisible();
    await dialog.getByRole('button', { name: 'Import', exact: true }).click();
    await expect(dialog.getByText('Import Complete')).toBeVisible();
    await dialog.getByRole('button', { name: 'Done' }).click();

    const contacts = await storedContacts(page);
    expect(contacts).toHaveLength(1);
    const stored = await (await page.request.get(`${API_BASE_URL}/contacts/${contacts[0].ID}`)).json();
    expect(stored).toMatchObject({ firstname: 'Csv', lastname: 'Person' });
    expect(stored.emails.map((e: { value: string }) => e.value)).toEqual(['csv@example.com']);
  });

  test('refuses a file that is neither CSV nor vCard', async ({ page }) => {
    await signInAsNewUser(page, uniqueUser('e2e_import_bad'));

    const dialog = await openImport(page);
    await chooseFile(page, 'notes.txt', 'text/plain', 'not contacts');
    await expect(dialog.getByRole('alert').filter({ hasText: 'valid CSV or VCF' })).toBeVisible();
    expect(await storedContacts(page)).toHaveLength(0);
  });
});
