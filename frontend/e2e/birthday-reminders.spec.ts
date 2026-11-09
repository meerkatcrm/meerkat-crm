import type { Page } from '@playwright/test';
import { test, expect, createTestContact, deleteTestContact, itemWithButton } from './fixtures';
import { API_BASE_URL } from './global-setup';

async function storedReminders(page: Page, contactId: number) {
  const response = await page.request.get(`${API_BASE_URL}/contacts/${contactId}/reminders`);
  return (await response.json()).reminders ?? [];
}

// A birthday reminder is a calendar date, so nothing here may depend on where the browser is.
for (const timezoneId of ['Europe/Berlin', 'America/Los_Angeles', 'Australia/Sydney', 'Pacific/Auckland']) {
  test.describe(`Birthday reminders in ${timezoneId}`, () => {
    test.use({ timezoneId });

    test('toggles on, follows a birthday change, and toggles off', async ({ page }) => {
      const contact = await createTestContact(page.request, { birthday: '1990-03-15' });
      const reminderSwitch = page.getByRole('switch', { name: 'Birthday reminder' });

      try {
        await page.goto(`/contacts/${contact.ID}`);
        await expect(reminderSwitch).not.toBeChecked();

        await reminderSwitch.click();
        await expect(reminderSwitch).toBeChecked();

        await expect.poll(() => storedReminders(page, contact.ID)).toHaveLength(1);
        const [created] = await storedReminders(page, contact.ID);
        expect(created.remind_at).toMatch(/^\d{4}-03-15T00:00:00Z$/);

        await page.reload();
        await expect(reminderSwitch).toBeChecked();

        await itemWithButton(page, 'Birthday', 'Edit')
          .getByRole('button', { name: 'Edit', exact: true })
          .click({ force: true });
        await page.getByPlaceholder(/DD\.MM/).fill('20.04.1990');
        const moved = page.waitForResponse(
          (r) => r.request().method() === 'PUT' && r.url().endsWith(`/reminders/${created.ID}`)
        );
        await page.getByRole('button', { name: 'Save', exact: true }).click();
        expect((await moved).ok()).toBeTruthy();

        await expect(reminderSwitch).toBeChecked();
        const [updated, ...others] = await storedReminders(page, contact.ID);
        expect(others).toHaveLength(0);
        expect(updated.ID).toBe(created.ID);
        expect(updated.remind_at).toMatch(/^\d{4}-04-20T00:00:00Z$/);
        expect(updated.message).toBe(created.message);
        expect(updated.recurrence).toBe('yearly');
        expect(updated.reoccur_from_completion).toBe(false);

        await reminderSwitch.click();
        await expect(reminderSwitch).not.toBeChecked();
        await expect.poll(() => storedReminders(page, contact.ID)).toHaveLength(0);
      } finally {
        await deleteTestContact(page.request, contact.ID);
      }
    });
  });
}
