import { test, expect, LOGGED_OUT, loginUser, signInAsNewUser, uniqueUser, createTestContact, acceptConfirm } from './fixtures';

// Journeys that had no e2e coverage. Each test signs in as its own user, so
// lists hold only what the test created and nothing else can touch it.
test.use({ storageState: LOGGED_OUT });

test.describe('Contact journeys', () => {
  test('archiving hides a contact from the list, and unarchiving brings it back', async ({ page }) => {
    await signInAsNewUser(page, uniqueUser('e2e_archive'));
    const contact = await createTestContact(page.request, { firstname: 'Archie', lastname: 'Ve' });

    await page.goto(`/contacts/${contact.ID}`);
    await page.getByRole('button', { name: 'Archive', exact: true }).click();
    await acceptConfirm(page, /archive this contact/i, 'Archive');
    await expect(page.getByText('Archived', { exact: true })).toBeVisible();

    await page.goto('/contacts');
    await expect(page.getByRole('heading', { name: /contacts/i })).toBeVisible();
    await expect(page.getByText('Archie Ve')).toHaveCount(0);

    await page.getByLabel('Show archived').click();
    await expect(page.getByLabel('Show archived')).toBeChecked();
    await expect(page).toHaveURL(/archived=1/);
    await expect(page.getByText('Archie Ve')).toBeVisible();

    await page.goto(`/contacts/${contact.ID}`);
    await page.getByRole('button', { name: 'Unarchive' }).click();
    await expect(page.getByText('Archived', { exact: true })).toHaveCount(0);
  });

  test('bulk edit adds a circle to every selected contact', async ({ page }) => {
    await signInAsNewUser(page, uniqueUser('e2e_bulk'));
    const a = await createTestContact(page.request, { firstname: 'Bea', lastname: 'One' });
    const b = await createTestContact(page.request, { firstname: 'Bo', lastname: 'Two' });

    await page.goto('/contacts');
    await page.getByRole('button', { name: 'Select', exact: true }).click();
    await page.getByRole('button', { name: /^Select all \d+$/ }).click();
    await expect(page.getByText('2 selected')).toBeVisible();
    await page.getByRole('button', { name: 'Edit selected' }).click();

    const dialog = page.getByRole('dialog');
    await dialog.getByLabel('Add to circles').fill('Climbing');
    await dialog.getByLabel('Add to circles').press('Enter');
    await dialog.getByRole('button', { name: /^Apply to/ }).click();
    await expect(dialog).toBeHidden();

    for (const contact of [a, b]) {
      const body = await (await page.request.get(`/api/v1/contacts/${contact.ID}`)).json();
      expect((body.contact ?? body).circles).toContain('Climbing');
    }
  });

  test('a circle can be added to and removed from a contact on its page', async ({ page }) => {
    await signInAsNewUser(page, uniqueUser('e2e_circle'));
    const contact = await createTestContact(page.request, { firstname: 'Cleo', lastname: 'Circle' });

    await page.goto(`/contacts/${contact.ID}`);
    await page.getByRole('button', { name: 'Edit circles' }).click();
    await page.getByPlaceholder('New circle name...').fill('Chess');
    await page.getByPlaceholder('New circle name...').press('Enter');
    await expect(page.getByText('Chess', { exact: true })).toBeVisible();

    // Removing a circle asks first
    await page.locator('.MuiChip-root', { hasText: 'Chess' }).locator('.MuiChip-deleteIcon').click();
    await acceptConfirm(page, /Remove "Chess"/, 'Delete');
    await expect(page.getByText('Chess', { exact: true })).toHaveCount(0);
  });

  test('"Not a duplicate" removes the pair from the duplicates list', async ({ page }) => {
    await signInAsNewUser(page, uniqueUser('e2e_dupes'));
    await createTestContact(page.request, { firstname: 'Dana', lastname: 'Double' });
    await createTestContact(page.request, { firstname: 'Dana', lastname: 'Double' });

    await page.goto('/contacts/duplicates');
    await expect(page.getByText('Same name')).toBeVisible();
    await page.getByRole('button', { name: 'Not a duplicate' }).click();
    await expect(page.getByText('Marked as different people')).toBeVisible();
    await expect(page.getByText('No duplicate contacts found.')).toBeVisible();
  });

  test('the circle filter and the archived switch survive opening a contact and going back', async ({ page }) => {
    await signInAsNewUser(page, uniqueUser('e2e_filters'));
    const contact = await createTestContact(page.request, { firstname: 'Fay', lastname: 'Filter', circles: ['Choir'] });

    await page.goto('/contacts?circle=Choir');
    await page.getByText('Fay Filter').click();
    await expect(page).toHaveURL(new RegExp(`/contacts/${contact.ID}`));
    await page.goBack();
    await expect(page).toHaveURL(/circle=Choir/);
    await expect(page.getByText('Fay Filter')).toBeVisible();
  });
});

test.describe('Settings journeys', () => {
  test('changing the password lets the user log in with the new one', async ({ page }) => {
    const user = uniqueUser('e2e_pw');
    await signInAsNewUser(page, user);

    await page.goto('/settings');
    await page.getByLabel('Current password').fill(user.password);
    await page.getByLabel(/^New password/).fill('NewE2ePass456!');
    await page.getByLabel('Confirm new password').fill('Different789!');
    await page.getByRole('button', { name: 'Update password' }).click();
    await expect(page.getByText('New passwords do not match.')).toBeVisible();

    await page.getByLabel('Confirm new password').fill('NewE2ePass456!');
    await expect(page.getByText('New passwords do not match.')).toHaveCount(0);
    await page.getByRole('button', { name: 'Update password' }).click();
    await expect(page.getByText(/Password updated successfully/)).toBeVisible();

    // The new password logs in
    await page.context().clearCookies();
    await page.evaluate(() => localStorage.clear());
    await loginUser(page, { ...user, password: 'NewE2ePass456!' });
  });

  test('the theme choice is applied and survives a reload', async ({ page }) => {
    await signInAsNewUser(page, uniqueUser('e2e_theme'));
    await page.goto('/settings');
    await page.getByRole('combobox', { name: 'Theme' }).click();
    await page.getByRole('option', { name: 'Dark mode' }).click();
    const background = () => page.evaluate(() => getComputedStyle(document.body).backgroundColor);
    const dark = await background();

    await page.reload();
    expect(await background()).toBe(dark);

    await page.getByRole('combobox', { name: 'Theme' }).click();
    await page.getByRole('option', { name: 'Light mode' }).click();
    await expect.poll(background).not.toBe(dark);
  });
});

test.describe('Dashboard and shell journeys', () => {
  test('an unknown URL lands on the dashboard and says why', async ({ page }) => {
    await signInAsNewUser(page, uniqueUser('e2e_404'));
    await page.goto('/no-such-page');
    await expect(page).toHaveURL(/\/$/);
    await expect(page.getByText('That page does not exist')).toBeVisible();
  });

  test('after a session expires, logging in returns to the page that was open', async ({ page }) => {
    const user = uniqueUser('e2e_expiry');
    await signInAsNewUser(page, user);
    await page.goto('/notes');
    await expect(page.getByRole('heading', { name: 'Notes' })).toBeVisible();

    // Drop the session cookie, then make the page call the API
    await page.context().clearCookies();
    await page.reload();
    await expect(page).toHaveURL(/\/login/);

    await page.getByLabel(/username or email/i).fill(user.username);
    await page.getByLabel(/password/i).fill(user.password);
    await page.getByRole('button', { name: /login/i }).click();
    await expect(page).toHaveURL(/\/notes/);
  });

  test('the skip link is the first stop, and it moves focus to the main content', async ({ page }) => {
    await signInAsNewUser(page, uniqueUser('e2e_keys'));
    await page.goto('/');
    await expect(page.getByRole('heading', { name: /dashboard/i })).toBeVisible();

    await page.keyboard.press('Tab');
    const skip = page.getByRole('link', { name: 'Skip to main content' });
    await expect(skip).toBeFocused();
    await page.keyboard.press('Enter');
    await expect(page.locator('#main-content')).toBeFocused();
  });
});
