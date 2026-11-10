import { test, expect, LOGGED_OUT, signInAsNewUser, uniqueUser, createTestContact, waitForLoading, acceptConfirm } from './fixtures';
import { API_BASE_URL } from './global-setup';
import type { Page } from '@playwright/test';

// Authenticated via the shared storageState (see playwright.config.ts).
test.describe('Dashboard', () => {
  test('should display dashboard', async ({ page }) => {
    await page.goto('/');
    await expect(page.getByRole('heading', { name: /dashboard/i })).toBeVisible();
  });

  test('should display three dashboard sections', async ({ page }) => {
    await page.goto('/');
    await waitForLoading(page);

    await expect(page.getByText('Upcoming Birthdays').first()).toBeVisible();
    await expect(page.getByText('Upcoming Reminders').first()).toBeVisible();
    await expect(page.getByText('Stay in Touch').first()).toBeVisible();
  });
});

const inDays = (days: number) => new Date(Date.now() + days * 24 * 60 * 60 * 1000);

async function addReminder(page: Page, contactId: number, message: string, recurrence: string) {
  const created = await page.request.post(`${API_BASE_URL}/contacts/${contactId}/reminders`, {
    data: { message, remind_at: inDays(1).toISOString(), recurrence, contact_id: contactId },
  });
  expect(created.ok(), await created.text()).toBeTruthy();
  const body = await created.json();
  return body.reminder ?? body;
}

// Each test gets its own user, so the sections only hold what the test put there.
test.describe('Dashboard content', () => {
  test.use({ storageState: LOGGED_OUT });

  test('a new user sees an empty message in each section', async ({ page }) => {
    await signInAsNewUser(page, uniqueUser('e2e_dash_empty'));
    await page.goto('/');
    await expect(page.getByText('No upcoming birthdays')).toBeVisible();
    await expect(page.getByText('No upcoming reminders')).toBeVisible();
    await expect(page.getByText('No contacts available')).toBeVisible();
  });

  test('shows upcoming birthdays and contacts, each linking to the contact', async ({ page }) => {
    await signInAsNewUser(page, uniqueUser('e2e_dash_birthday'));
    const date = inDays(3).toISOString().slice(5, 10);
    const contact = await createTestContact(page.request, {
      firstname: 'Birthday',
      lastname: 'Person',
      // A leap year, so this is a valid date even when it lands on February 29.
      birthday: `1992-${date}`,
      circles: ['Friends'],
    });

    await page.goto('/');
    const cards = page.getByRole('link').filter({ hasText: 'Birthday Person' });
    await expect(cards).toHaveCount(2);
    const [month, day] = date.split('-');
    await expect(cards.first()).toContainText(`${day}.${month}.`);
    await expect(cards.last()).toContainText('Friends');

    await cards.first().click();
    await expect(page).toHaveURL(new RegExp(`/contacts/${contact.ID}$`));
  });

  test('completing a one-time reminder removes it', async ({ page }) => {
    await signInAsNewUser(page, uniqueUser('e2e_dash_complete'));
    const contact = await createTestContact(page.request, { firstname: 'Reminded', lastname: 'Person' });
    await addReminder(page, contact.ID, 'E2E dashboard once', 'once');

    await page.goto('/');
    await expect(page.getByText('E2E dashboard once')).toBeVisible();
    await page.getByRole('button', { name: 'Complete', exact: true }).click();
    await expect(page.getByText('E2E dashboard once')).toHaveCount(0);

    await page.reload();
    await expect(page.getByText('No upcoming reminders')).toBeVisible();
    const completions = await (await page.request.get(`${API_BASE_URL}/contacts/${contact.ID}/reminder-completions`)).json();
    expect(completions.completions ?? completions).toHaveLength(1);
  });

  test('skipping a recurring reminder reschedules it without recording a completion', async ({ page }) => {
    await signInAsNewUser(page, uniqueUser('e2e_dash_skip'));
    const contact = await createTestContact(page.request, { firstname: 'Skipped', lastname: 'Person' });
    const reminder = await addReminder(page, contact.ID, 'E2E dashboard monthly', 'monthly');
    const scheduled = new Date(reminder.remind_at).getTime();

    await page.goto('/');
    await expect(page.getByText('E2E dashboard monthly')).toBeVisible();
    await page.getByRole('button', { name: 'Skip', exact: true }).click();
    await acceptConfirm(page, /skip this reminder/i, 'Skip');

    await expect
      .poll(async () => {
        const body = await (await page.request.get(`${API_BASE_URL}/reminders/${reminder.ID}`)).json();
        return new Date((body.reminder ?? body).remind_at).getTime();
      })
      .toBeGreaterThan(scheduled);
    const completions = await (await page.request.get(`${API_BASE_URL}/contacts/${contact.ID}/reminder-completions`)).json();
    expect(completions.completions ?? completions).toHaveLength(0);
  });

  test('selecting a reminder opens its contact', async ({ page }) => {
    await signInAsNewUser(page, uniqueUser('e2e_dash_open'));
    const contact = await createTestContact(page.request, { firstname: 'Opened', lastname: 'Person' });
    await addReminder(page, contact.ID, 'E2E dashboard open', 'once');

    await page.goto('/');
    await page.getByText('E2E dashboard open').click();
    await expect(page).toHaveURL(new RegExp(`/contacts/${contact.ID}$`));
  });
});

test.describe('Navigation', () => {
  test('should navigate to contacts from sidebar', async ({ page }) => {
    await page.goto('/');
    await page.getByRole('link', { name: /contacts/i }).click();
    await expect(page).toHaveURL(/\/contacts/);
    await expect(page.getByRole('heading', { name: /contacts/i })).toBeVisible();
  });

  test('should navigate to activities from sidebar', async ({ page }) => {
    await page.goto('/');
    await page.getByRole('link', { name: /activities/i }).click();
    await expect(page).toHaveURL(/\/activities/);
    await expect(page.getByRole('heading', { name: /activities/i })).toBeVisible();
  });

  test('should navigate to settings from sidebar', async ({ page }) => {
    await page.goto('/');
    // Settings is a collapsible submenu — expand it, then open Profile.
    await page.getByRole('button', { name: 'Settings', exact: true }).click();
    await page.getByRole('link', { name: 'Profile', exact: true }).click();
    await expect(page).toHaveURL(/\/settings/);
    await expect(page.getByRole('heading', { name: /settings/i }).first()).toBeVisible();
  });
});
