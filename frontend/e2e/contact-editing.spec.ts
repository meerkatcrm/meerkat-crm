import type { Page } from '@playwright/test';
import { test, expect, createTestContact, deleteTestContact, itemWithButton } from './fixtures';
import { API_BASE_URL } from './global-setup';

// The edit pencils only appear on hover, which is why the clicks are forced.
async function startEditing(page: Page, label: string) {
  await itemWithButton(page, label, 'Edit').getByRole('button', { name: 'Edit', exact: true }).click({ force: true });
}

async function save(page: Page, contactId: number) {
  const saved = page.waitForResponse(
    (r) => r.request().method() === 'PUT' && r.url().endsWith(`/contacts/${contactId}`)
  );
  await page.getByRole('button', { name: 'Save', exact: true }).click();
  expect((await saved).ok()).toBeTruthy();
}

async function stored(page: Page, contactId: number) {
  return (await page.request.get(`${API_BASE_URL}/contacts/${contactId}`)).json();
}

test.describe('Inline contact editing', () => {
  test('adds, changes and removes an email', async ({ page }) => {
    const contact = await createTestContact(page.request);

    try {
      await page.goto(`/contacts/${contact.ID}`);
      await startEditing(page, 'Email');
      await page.getByRole('button', { name: 'Add', exact: true }).click();
      await page.getByRole('textbox', { name: 'Email', exact: true }).fill('first@example.com');
      await save(page, contact.ID);

      await page.reload();
      await expect(page.getByText('first@example.com')).toBeVisible();

      await startEditing(page, 'Email');
      await page.getByRole('textbox', { name: 'Email', exact: true }).fill('second@example.com');
      await save(page, contact.ID);

      await page.reload();
      await expect(page.getByText('second@example.com')).toBeVisible();
      await expect(page.getByText('first@example.com')).toHaveCount(0);

      await startEditing(page, 'Email');
      await page.getByRole('button', { name: 'Delete', exact: true }).click();
      await save(page, contact.ID);

      await page.reload();
      await expect(page.getByRole('tab', { name: /reminders/i })).toBeVisible();
      await expect(page.getByText('second@example.com')).toHaveCount(0);
      expect((await stored(page, contact.ID)).emails ?? []).toEqual([]);
    } finally {
      await deleteTestContact(page.request, contact.ID);
    }
  });

  test('saving a phone keeps the emails already stored', async ({ page }) => {
    const contact = await createTestContact(page.request, {
      emails: [{ type: 'home', value: 'kept@example.com' }],
    });

    try {
      await page.goto(`/contacts/${contact.ID}`);
      await startEditing(page, 'Phone');
      await page.getByRole('button', { name: 'Add', exact: true }).click();
      await page.getByRole('textbox', { name: 'Phone', exact: true }).fill('+1 555-0123');
      await save(page, contact.ID);

      await page.reload();
      await expect(page.getByText('+1 555-0123')).toBeVisible();
      await expect(page.getByText('kept@example.com')).toBeVisible();

      const body = await stored(page, contact.ID);
      expect(body.phones.map((p: { value: string }) => p.value)).toEqual(['+1 555-0123']);
      expect(body.emails.map((e: { value: string }) => e.value)).toEqual(['kept@example.com']);
    } finally {
      await deleteTestContact(page.request, contact.ID);
    }
  });

  test('saves every part of an address', async ({ page }) => {
    const contact = await createTestContact(page.request);

    try {
      await page.goto(`/contacts/${contact.ID}`);
      await startEditing(page, 'Address');
      await page.getByRole('button', { name: 'Add', exact: true }).click();
      await page.getByRole('textbox', { name: 'Street', exact: true }).fill('1 Burrow Lane');
      await page.getByRole('textbox', { name: 'City', exact: true }).fill('Kalahari');
      await page.getByRole('textbox', { name: 'State / Region', exact: true }).fill('Northern Cape');
      await page.getByRole('textbox', { name: 'Postal Code', exact: true }).fill('8800');
      await page.getByRole('textbox', { name: 'Country', exact: true }).fill('South Africa');
      await save(page, contact.ID);

      await page.reload();
      await expect(page.getByText(/1 Burrow Lane/)).toBeVisible();

      expect((await stored(page, contact.ID)).addresses[0]).toMatchObject({
        street: '1 Burrow Lane',
        city: 'Kalahari',
        region: 'Northern Cape',
        postal: '8800',
        country: 'South Africa',
      });
    } finally {
      await deleteTestContact(page.request, contact.ID);
    }
  });

  test('rejects an invalid birthday, then saves a valid one', async ({ page }) => {
    const contact = await createTestContact(page.request);

    try {
      await page.goto(`/contacts/${contact.ID}`);
      await startEditing(page, 'Birthday');
      const input = page.getByPlaceholder(/DD\.MM/);

      await input.fill('31.31.1990');
      await page.getByRole('button', { name: 'Save', exact: true }).click();
      await expect(page.getByText(/invalid date format/i)).toBeVisible();
      expect((await stored(page, contact.ID)).birthday ?? '').toBe('');

      await input.fill('22.07.1985');
      await save(page, contact.ID);

      await page.reload();
      await expect(page.getByText(/22\.07\.1985/)).toBeVisible();
      expect((await stored(page, contact.ID)).birthday).toBe('1985-07-22');
    } finally {
      await deleteTestContact(page.request, contact.ID);
    }
  });

  test('cancelling an edit discards the change', async ({ page }) => {
    const contact = await createTestContact(page.request, {
      emails: [{ type: 'home', value: 'unchanged@example.com' }],
    });

    try {
      await page.goto(`/contacts/${contact.ID}`);
      await startEditing(page, 'Email');
      await page.getByRole('textbox', { name: 'Email', exact: true }).fill('discarded@example.com');
      await page.getByRole('button', { name: 'Cancel', exact: true }).click();

      await expect(page.getByText('unchanged@example.com')).toBeVisible();
      await page.reload();
      await expect(page.getByText('unchanged@example.com')).toBeVisible();
      await expect(page.getByText('discarded@example.com')).toHaveCount(0);
    } finally {
      await deleteTestContact(page.request, contact.ID);
    }
  });
});
