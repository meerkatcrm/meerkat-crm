import { test, expect, vi, beforeEach, afterEach } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import i18n from '../i18n/config';
import ErrorBoundary from './ErrorBoundary';

let broken = true;
function Flaky() {
  if (broken) throw new Error('kaboom');
  return <div>all good</div>;
}

beforeEach(() => {
  broken = true;
  vi.spyOn(console, 'error').mockImplementation(() => {});
});

afterEach(async () => {
  cleanup();
  vi.restoreAllMocks();
  await i18n.changeLanguage('en');
});

test('renders children when nothing throws', () => {
  broken = false;
  render(<ErrorBoundary><Flaky /></ErrorBoundary>);
  expect(screen.getByText('all good')).toBeInTheDocument();
});

test('shows the translated fallback, reports the error, and recovers on retry', async () => {
  await i18n.changeLanguage('en');
  const onError = vi.fn();
  render(<ErrorBoundary onError={onError}><Flaky /></ErrorBoundary>);

  expect(screen.getByRole('heading', { name: 'Oops! Something went wrong' })).toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Reload Page' })).toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Go Home' })).toBeInTheDocument();
  expect(screen.queryByText(/kaboom/)).not.toBeInTheDocument();
  expect(onError).toHaveBeenCalledWith(expect.objectContaining({ message: 'kaboom' }), expect.anything());

  broken = false;
  fireEvent.click(screen.getByRole('button', { name: 'Try Again' }));
  expect(screen.getByText('all good')).toBeInTheDocument();
});

test('follows the UI language', async () => {
  await i18n.changeLanguage('de');
  render(<ErrorBoundary><Flaky /></ErrorBoundary>);
  expect(screen.getByRole('heading', { name: 'Hoppla! Etwas ist schiefgelaufen' })).toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Erneut versuchen' })).toBeInTheDocument();
});

test('showDetails adds the error text', () => {
  render(<ErrorBoundary showDetails><Flaky /></ErrorBoundary>);
  expect(screen.getByText('Error Details (Development Only)')).toBeInTheDocument();
  expect(screen.getByText(/kaboom/)).toBeInTheDocument();
});
