import type { Page } from '@playwright/test';
import { test, expect, LOGGED_OUT, signInAsNewUser, uniqueUser, createTestContact, listContacts } from './fixtures';
import { API_BASE_URL } from './global-setup';

// http://fixtures/ is the static server in docker-compose.test.yml.
const CALENDAR_URL = 'http://fixtures/calendar.ics';
// Port 8080 is the app's own port inside its container, so the backend syncs against itself.
const ownCardDAVUrl = (username: string) => `http://127.0.0.1:8080/carddav/principals/${username}/`;

test.use({ storageState: LOGGED_OUT });

async function addCalendar(page: Page, name: string, url: string) {
  await page.getByRole('button', { name: 'Add calendar' }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByRole('textbox', { name: /^Name/ }).fill(name);
  await dialog.getByRole('textbox', { name: /^Calendar URL/ }).fill(url);
  // The fixture event is dated October 2026, so this window (the maximum) keeps
  // it inside until October 2036. Move the date in calendar.ics before then.
  await dialog.getByRole('spinbutton', { name: 'Days in the past' }).fill('3650');
  await dialog.getByRole('button', { name: 'Save' }).click();
  await expect(dialog).toBeHidden();
}

test.describe('Calendar sync', () => {
  test('imports events as activities, without duplicates on a second sync', async ({ page }) => {
    await signInAsNewUser(page, uniqueUser('e2e_calendar'));
    await page.goto('/settings/data');

    await addCalendar(page, 'E2E calendar', CALENDAR_URL);
    await expect(page.getByRole('alert').filter({ hasText: 'Sync complete: 1 created' })).toBeVisible({ timeout: 15000 });

    const row = page.getByRole('listitem').filter({ hasText: 'E2E calendar' });
    await row.getByRole('button', { name: 'Sync now' }).click();
    await expect(page.getByRole('alert').filter({ hasText: '0 created, 0 updated, 1 unchanged' })).toBeVisible({ timeout: 15000 });

    await page.goto('/activities');
    await expect(page.getByText('E2E fixture meeting')).toHaveCount(1);
  });

  test('marks a calendar whose feed fails, and it can be deleted', async ({ page }) => {
    await signInAsNewUser(page, uniqueUser('e2e_calendar_bad'));
    await page.goto('/settings/data');

    await addCalendar(page, 'Broken calendar', 'http://fixtures/missing.ics');
    const row = page.getByRole('listitem').filter({ hasText: 'Broken calendar' });
    await expect(row.getByText('Sync failed')).toBeVisible({ timeout: 15000 });

    await page.reload();
    await expect(row.getByText('Sync failed')).toBeVisible();

    await row.getByRole('button', { name: 'Delete', exact: true }).click();
    await page.getByRole('dialog').getByRole('button', { name: 'Delete' }).click();
    await expect(row).toHaveCount(0);

    await page.reload();
    await expect(page.getByText('No calendars connected yet.')).toBeVisible();
  });
});

test.describe('Contact sync and the phone sync filter', () => {
  test('a connected server only hands over the contacts its filter allows', async ({ page }) => {
    // The remote account: two contacts, and a filter that lets one of them through.
    const remote = uniqueUser('e2e_dav_remote');
    await signInAsNewUser(page, remote);
    await createTestContact(page.request, { firstname: 'Allowed', lastname: 'Friend', circles: ['Friends'] });
    await createTestContact(page.request, { firstname: 'Blocked', lastname: 'Relative', circles: ['Family'] });

    await page.goto('/settings/data');
    await page.getByRole('radio', { name: 'Only sync contacts in these circles' }).check();
    await page.getByRole('combobox', { name: 'Circles' }).click();
    await page.getByRole('option', { name: 'Friends', exact: true }).click();
    await page.keyboard.press('Escape');
    await page.getByRole('button', { name: 'Save filter' }).click();
    await expect(page.getByRole('alert').filter({ hasText: 'Filter saved' })).toBeVisible();

    await page.reload();
    await expect(page.getByRole('radio', { name: 'Only sync contacts in these circles' })).toBeChecked();

    await page.getByRole('button', { name: 'Logout' }).click();
    await expect(page.getByRole('heading', { name: /login/i })).toBeVisible();

    // The local account connects to the remote one over CardDAV.
    const local = uniqueUser('e2e_dav_local');
    await signInAsNewUser(page, local);
    await page.goto('/settings/data');
    await page.getByRole('button', { name: 'Connect server' }).click();
    const dialog = page.getByRole('dialog');
    await dialog.getByRole('textbox', { name: /^Server URL/ }).fill(ownCardDAVUrl(remote.username));
    await dialog.getByRole('textbox', { name: 'Username' }).fill(remote.username);
    await dialog.getByLabel('Password').fill(remote.password);
    await dialog.getByRole('button', { name: 'Next' }).click();

    await dialog.getByRole('combobox', { name: 'Sync direction' }).click();
    await page.getByRole('option', { name: /^Import only/ }).click();
    await dialog.getByRole('button', { name: 'Save' }).click();
    await expect(dialog).toBeHidden({ timeout: 15000 });

    // Read the outcome from the API: the page only announces a sync it saw running.
    await expect
      .poll(
        async () => {
          const body = await (await page.request.get(`${API_BASE_URL}/carddav/connection`)).json();
          const connection = body.connection ?? body;
          return connection.sync_running ? 'running' : connection.last_sync_status;
        },
        { timeout: 20000 }
      )
      .toBe('success');
    const contacts = await listContacts(page.request);
    expect(contacts.map((c: { firstname: string }) => c.firstname)).toEqual(['Allowed']);

    await page.goto('/contacts');
    await expect(page.getByText('Allowed Friend')).toBeVisible();
    await expect(page.getByText('Blocked Relative')).toHaveCount(0);

    // Disconnecting keeps what was imported.
    await page.goto('/settings/data');
    await page.getByRole('button', { name: 'Disconnect' }).click();
    await page.getByRole('dialog').getByRole('button', { name: 'Disconnect' }).click();
    await expect(page.getByRole('button', { name: 'Connect server' })).toBeVisible();
    expect(await listContacts(page.request)).toHaveLength(1);
  });
});
