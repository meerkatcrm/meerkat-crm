import { request } from '@playwright/test';
import { test, expect, LOGGED_OUT, loginAsNewUser, uniqueUser } from './fixtures';
import { API_BASE_URL, TEST_USER } from './global-setup';

// The first account registered on a stack becomes its admin, and global setup
// registers the seeded user first on the fresh stack the suite expects.
test.describe('Admin users', () => {
  test('an admin can see and delete a user', async ({ page }) => {
    const user = uniqueUser('e2e_admin_target');
    const context = await loginAsNewUser(user);
    await context.dispose();

    await page.goto('/users');
    await expect(page.getByRole('row').filter({ hasText: TEST_USER.username })).toBeVisible();

    // Users list oldest first in pages, so the new one may be further along.
    const row = page.getByRole('row').filter({ hasText: user.username });
    await expect(async () => {
      if (!(await row.isVisible())) {
        await page.getByRole('button', { name: /next page/i }).click({ timeout: 1000 });
      }
      await expect(row).toBeVisible({ timeout: 500 });
    }).toPass();

    await row.getByRole('button', { name: 'Delete' }).click();
    const dialog = page.getByRole('dialog');
    await dialog.getByRole('button', { name: 'Delete' }).click();
    // An open dialog hides the page from role locators, so the row count alone
    // reads 0 before the delete has finished. The dialog closes once it has.
    await expect(dialog).toBeHidden();
    await expect(row).toHaveCount(0);

    const anonymous = await request.newContext({ storageState: LOGGED_OUT });
    const login = await anonymous.post(`${API_BASE_URL}/login`, {
      data: { identifier: user.username, password: user.password },
    });
    expect(login.status()).toBe(401);
    await anonymous.dispose();

    await page.reload();
    await expect(page.getByRole('row').filter({ hasText: TEST_USER.username })).toBeVisible();
  });
});
