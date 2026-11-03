import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';
import { cleanup, render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import i18n from './i18n/config';
import LoginPage from './LoginPage';

const LOCALES = ['en', 'de', 'fr', 'es', 'it'];

describe('repository moved notice', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false }));
  });

  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  test.each(LOCALES)('shows the old and new image paths and the new repository in %s', async (locale) => {
    await i18n.changeLanguage(locale);
    render(
      <MemoryRouter>
        <LoginPage />
      </MemoryRouter>
    );

    expect(screen.getByText(i18n.t('login.repoMovedTitle'))).toBeInTheDocument();
    const alert = screen.getByRole('alert');
    expect(alert).toHaveTextContent('ghcr.io/fbuchner/');
    expect(alert).toHaveTextContent('ghcr.io/meerkatcrm/');
    expect(screen.getByRole('link', { name: i18n.t('login.repoMovedLink') })).toHaveAttribute(
      'href',
      'https://github.com/meerkatcrm/meerkat-crm'
    );
    expect(i18n.getResource(locale, 'translation', 'login.repoMovedTitle')).toBeTruthy();
    expect(i18n.getResource(locale, 'translation', 'login.repoMovedBody')).toBeTruthy();
    expect(i18n.getResource(locale, 'translation', 'login.repoMovedLink')).toBeTruthy();
  });
});
