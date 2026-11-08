import { test, expect, LOGGED_OUT, signInAsNewUser, uniqueUser } from './fixtures';
import { API_BASE_URL } from './global-setup';

// http://fixtures/ serves a small Monica API from e2e/fixture-server (see docker-compose.test.yml).
test.use({ storageState: LOGGED_OUT });

// The client limits itself to about one request a second, so these flows take a while.
const MONICA_TIMEOUT = 40000;

test.describe('Monica import', () => {
  test('imports contacts and their notes, activities and reminders', async ({ page }) => {
    test.slow();
    await signInAsNewUser(page, uniqueUser('e2e_monica'));

    await page.goto('/settings/data');
    await page.getByRole('button', { name: 'Start import' }).click();
    const dialog = page.getByRole('dialog');
    await dialog.getByRole('textbox', { name: /^Monica URL/ }).fill('http://fixtures/');
    await dialog.getByLabel(/^API token/).fill('e2e-token');
    await dialog.getByRole('button', { name: 'Connect' }).click();

    await expect(dialog.getByText('2 contacts')).toBeVisible({ timeout: MONICA_TIMEOUT });
    await expect(dialog.getByText('1 activities')).toBeVisible();
    await dialog.getByRole('button', { name: 'Fetch data' }).click();

    await expect(dialog.getByText('2 to create')).toBeVisible({ timeout: MONICA_TIMEOUT });
    await dialog.getByRole('button', { name: 'Import', exact: true }).click();
    const done = dialog.getByRole('button', { name: 'Done' });
    await expect(done).toBeVisible({ timeout: MONICA_TIMEOUT });
    await done.click();

    const contacts = (await (await page.request.get(`${API_BASE_URL}/contacts?limit=10`)).json()).contacts;
    expect(contacts.map((c: { firstname: string }) => c.firstname).sort()).toEqual(['Max', 'Mona']);

    const mona = contacts.find((c: { firstname: string }) => c.firstname === 'Mona');
    await page.goto(`/contacts/${mona.ID}`);
    await expect(page.getByRole('heading', { name: 'Mona Imported' })).toBeVisible();
    await expect(page.getByText('mona@example.com')).toBeVisible();
    await expect(page.getByText('E2E Monica note')).toBeVisible();
    await expect(page.getByText('E2E Monica lunch')).toBeVisible();
    await page.getByRole('tab', { name: /reminders/i }).click();
    await expect(page.getByText('E2E Monica reminder')).toBeVisible();
  });

  test('shows an error and stays on the first step when Monica cannot be reached', async ({ page }) => {
    await signInAsNewUser(page, uniqueUser('e2e_monica_bad'));

    await page.goto('/settings/data');
    await page.getByRole('button', { name: 'Start import' }).click();
    const dialog = page.getByRole('dialog');
    await dialog.getByRole('textbox', { name: /^Monica URL/ }).fill('http://fixtures/missing/');
    await dialog.getByLabel(/^API token/).fill('e2e-token');
    await dialog.getByRole('button', { name: 'Connect' }).click();

    await expect(dialog.getByRole('alert').filter({ hasNotText: 'API token' })).toBeVisible({ timeout: 15000 });
    await expect(dialog.getByRole('button', { name: 'Fetch data' })).toHaveCount(0);
    const contacts = (await (await page.request.get(`${API_BASE_URL}/contacts?limit=10`)).json()).contacts;
    expect(contacts).toHaveLength(0);
  });
});
