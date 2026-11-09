import { test, expect, createTestContact, deleteTestContact } from './fixtures';

// Narrow CSS widths are also what a desktop browser gets at high zoom: 600 px is
// 240% on a 1440 px display. A page must never scroll sideways as a whole. Wide
// content such as a data table scrolls inside its own container instead.
const PAGES = ['/network', '/users', '/settings', '/settings/data', '/api-tokens', '/activities'];
const WIDTHS = [600, 375];

// German labels run longest, so they are the first to overflow or clip.
for (const language of ['en', 'de']) {
  test.describe(`Narrow layout (${language})`, () => {
    test.beforeEach(async ({ page }) => {
      await page.addInitScript((lng) => localStorage.setItem('i18nextLng', lng), language);
    });

    for (const width of WIDTHS) {
      test(`pages do not scroll sideways at ${width} px`, async ({ page }) => {
        await page.setViewportSize({ width, height: 800 });

        const contact = await createTestContact(page.request);
        try {
          for (const path of [...PAGES, `/contacts/${contact.ID}`]) {
            await page.goto(path);
            await page.waitForLoadState('networkidle');
            // A redirect, such as /users without admin rights, would measure the wrong page.
            await expect(page).toHaveURL(path);
            const overflow = await page.evaluate(
              () => document.documentElement.scrollWidth - document.documentElement.clientWidth
            );
            expect(overflow, `${path} overflows the page by ${overflow} px`).toBe(0);
          }
        } finally {
          await deleteTestContact(page.request, contact.ID);
        }
      });
    }
  });
}

test('the logout button keeps its accessible name at phone width', async ({ page }) => {
  await page.setViewportSize({ width: 375, height: 800 });
  await page.goto('/settings');
  await expect(page.getByRole('button', { name: 'Logout' })).toBeVisible();
});
