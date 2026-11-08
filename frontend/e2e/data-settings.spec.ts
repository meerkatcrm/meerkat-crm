import { test, expect, LOGGED_OUT, signInAsNewUser, uniqueUser, createTestContact, itemWithButton } from './fixtures';
import { API_BASE_URL } from './global-setup';

// These settings belong to the user, so each test gets its own.
test.use({ storageState: LOGGED_OUT });

test.describe('Contact field settings', () => {
  test('a disabled field disappears from contacts but keeps its data', async ({ page }) => {
    await signInAsNewUser(page, uniqueUser('e2e_fields'));
    const contact = await createTestContact(page.request, { birthday: '1990-03-15' });

    await page.goto(`/contacts/${contact.ID}`);
    await expect(page.getByText('Birthday', { exact: true })).toBeVisible();

    await page.goto('/settings/data');
    const birthday = page.getByRole('switch', { name: 'Birthday', exact: true });
    await expect(birthday).toBeChecked();
    await birthday.click();
    await expect(birthday).toBeEnabled();
    await expect(birthday).not.toBeChecked();

    await page.reload();
    await expect(page.getByRole('switch', { name: 'Birthday', exact: true })).not.toBeChecked();

    await page.goto(`/contacts/${contact.ID}`);
    await expect(page.getByRole('tab', { name: /reminders/i })).toBeVisible();
    await expect(page.getByText('Birthday', { exact: true })).toHaveCount(0);
    const stored = await (await page.request.get(`${API_BASE_URL}/contacts/${contact.ID}`)).json();
    expect(stored.birthday).toBe('1990-03-15');
  });

  test('hiding empty fields keeps them one click away', async ({ page }) => {
    await signInAsNewUser(page, uniqueUser('e2e_hideempty'));
    const contact = await createTestContact(page.request, {
      emails: [{ type: 'home', value: 'filled@example.com' }],
    });

    await page.goto('/settings/data');
    const hideEmpty = page.getByRole('switch', { name: 'Only show filled-in fields' });
    await hideEmpty.click();
    await expect(hideEmpty).toBeChecked();
    await expect(hideEmpty).toBeEnabled();

    await page.goto(`/contacts/${contact.ID}`);
    await expect(page.getByText('filled@example.com')).toBeVisible();
    await expect(page.getByText('Phone', { exact: true })).toHaveCount(0);

    await page.getByRole('button', { name: /show all fields/i }).click();
    await expect(page.getByText('Phone', { exact: true })).toBeVisible();
  });
});

test.describe('Custom fields', () => {
  test('a defined field can be ordered, filled in on a contact, and removed', async ({ page }) => {
    await signInAsNewUser(page, uniqueUser('e2e_custom'));
    const contact = await createTestContact(page.request);

    await page.goto('/settings/data');
    const newField = page.getByPlaceholder('New field name...');
    const add = page.getByRole('button', { name: 'Add', exact: true });
    await newField.fill('Favourite snack');
    await add.click();
    await expect(page.getByRole('listitem').filter({ hasText: 'Favourite snack' })).toBeVisible();
    await newField.fill('Shoe size');
    await add.click();
    const shoeSize = page.getByRole('listitem').filter({ hasText: 'Shoe size' });
    await expect(shoeSize).toBeVisible();

    await newField.fill('shoe size');
    await add.click();
    await expect(page.getByText('A field with this name already exists.')).toBeVisible();
    await newField.clear();

    await shoeSize.getByRole('button', { name: 'Move up' }).click();
    await expect(shoeSize.getByRole('button', { name: 'Move up' })).toBeDisabled();
    await page.reload();
    await expect(page.getByRole('listitem').filter({ hasText: /Shoe size|Favourite snack/ }).first()).toContainText('Shoe size');

    await page.goto(`/contacts/${contact.ID}`);
    await itemWithButton(page, 'Favourite snack', 'Edit').getByRole('button', { name: 'Edit', exact: true }).click({ force: true });
    await page.getByRole('textbox').last().fill('Scorpions');
    await page.getByRole('button', { name: 'Save', exact: true }).click();
    await expect(page.getByText('Scorpions')).toBeVisible();

    await page.reload();
    await expect(page.getByText('Scorpions')).toBeVisible();
    const stored = await (await page.request.get(`${API_BASE_URL}/contacts/${contact.ID}`)).json();
    expect(stored.custom_fields).toMatchObject({ 'Favourite snack': 'Scorpions' });

    await page.goto('/settings/data');
    await page.getByRole('listitem').filter({ hasText: 'Favourite snack' }).getByRole('button', { name: 'Delete' }).click();
    await page.getByRole('dialog').getByRole('button', { name: 'Delete' }).click();
    await expect(page.getByRole('listitem').filter({ hasText: 'Favourite snack' })).toHaveCount(0);

    await page.goto(`/contacts/${contact.ID}`);
    await expect(page.getByRole('tab', { name: /reminders/i })).toBeVisible();
    await expect(page.getByText('Favourite snack')).toHaveCount(0);
  });
});
