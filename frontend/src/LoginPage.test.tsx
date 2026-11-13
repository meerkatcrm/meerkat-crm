import { test, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, cleanup } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import './i18n/config';
import LoginPage from './LoginPage';
import RegisterPage from './RegisterPage';
import { parseAuthConfig, DEFAULT_AUTH_CONFIG } from './hooks/useAuthConfig';

const fetchMock = vi.fn();

// The server's answer to GET /auth/config (null: the request fails).
function serveAuthConfig(config: object | null) {
  fetchMock.mockImplementation(async (url: string) =>
    url.endsWith('/auth/config') && config
      ? { ok: true, status: 200, json: async () => config }
      : { ok: false, status: 500, json: async () => ({}) }
  );
}

const renderAt = (ui: React.ReactElement) => render(<MemoryRouter>{ui}</MemoryRouter>);
const authConfigFetched = () =>
  vi.waitFor(() => expect(fetchMock).toHaveBeenCalledWith(expect.stringContaining('/auth/config'), expect.anything()));

beforeEach(() => {
  fetchMock.mockReset();
  vi.stubGlobal('fetch', fetchMock);
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

test('password and SSO together: both offered, with the configured provider name', async () => {
  serveAuthConfig({ oidc_enabled: true, oidc_provider_name: 'Pocket ID', password_login_enabled: true, registration_enabled: true });
  renderAt(<LoginPage />);
  expect(await screen.findByRole('button', { name: 'Login with Pocket ID' })).toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Login' })).toBeInTheDocument();
  expect(screen.getByRole('link', { name: /Register/ })).toBeInTheDocument();
});

test('registration disabled hides the register link but keeps the password form', async () => {
  serveAuthConfig({ oidc_enabled: false, password_login_enabled: true, registration_enabled: false });
  renderAt(<LoginPage />);
  await authConfigFetched();
  await vi.waitFor(() => expect(screen.queryByRole('link', { name: /Register/ })).not.toBeInTheDocument());
  expect(screen.getByRole('button', { name: 'Login' })).toBeInTheDocument();
  expect(screen.getByRole('button', { name: /Forgot your password/ })).toBeInTheDocument();
});

test('SSO-only shows just the SSO button', async () => {
  serveAuthConfig({ oidc_enabled: true, oidc_provider_name: 'Keycloak', password_login_enabled: false, registration_enabled: false });
  renderAt(<LoginPage />);
  expect(await screen.findByRole('button', { name: 'Login with Keycloak' })).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Login' })).not.toBeInTheDocument();
  expect(screen.queryByLabelText(/Password/)).not.toBeInTheDocument();
  expect(screen.queryByRole('button', { name: /Forgot your password/ })).not.toBeInTheDocument();
  expect(screen.queryByRole('link', { name: /Register/ })).not.toBeInTheDocument();
  expect(screen.queryByText('or')).not.toBeInTheDocument();
});

test('a failed config request keeps password sign-in available', async () => {
  serveAuthConfig(null);
  renderAt(<LoginPage />);
  await authConfigFetched();
  expect(screen.getByRole('button', { name: 'Login' })).toBeInTheDocument();
  expect(screen.getByRole('link', { name: /Register/ })).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: /Login with/ })).not.toBeInTheDocument();
});

test('register page explains when registration is disabled', async () => {
  serveAuthConfig({ oidc_enabled: false, password_login_enabled: true, registration_enabled: false });
  renderAt(<RegisterPage />);
  expect(await screen.findByText(/Registration is disabled on this instance/)).toBeInTheDocument();
  expect(screen.getByRole('link', { name: 'Back to login' })).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Register' })).not.toBeInTheDocument();
});

test('register page shows the form when registration is open', async () => {
  serveAuthConfig({ oidc_enabled: false, password_login_enabled: true, registration_enabled: true });
  renderAt(<RegisterPage />);
  await authConfigFetched();
  expect(await screen.findByRole('button', { name: 'Register' })).toBeInTheDocument();
  expect(screen.queryByText(/Registration is disabled/)).not.toBeInTheDocument();
});

test('parseAuthConfig never turns off password sign-in without SSO, and defaults safely', () => {
  expect(parseAuthConfig(null)).toEqual(DEFAULT_AUTH_CONFIG);
  expect(parseAuthConfig({ oidc_enabled: false, password_login_enabled: false }).passwordLoginEnabled).toBe(true);
  expect(parseAuthConfig({ oidc_enabled: true, password_login_enabled: false }).passwordLoginEnabled).toBe(false);
  expect(parseAuthConfig({ oidc_enabled: true }).oidcProviderName).toBe('SSO');
  expect(parseAuthConfig({}).registrationEnabled).toBe(true);
});
