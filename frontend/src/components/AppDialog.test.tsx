import { test, expect, vi, afterEach } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import AppDialog from './AppDialog';

afterEach(cleanup);

test('Esc closes the dialog', () => {
  const onClose = vi.fn();
  render(<AppDialog open onClose={onClose}>content</AppDialog>);
  fireEvent.keyDown(screen.getByRole('dialog'), { key: 'Escape' });
  expect(onClose).toHaveBeenCalledTimes(1);
});

test('Esc is ignored while the dialog is busy', () => {
  const onClose = vi.fn();
  render(<AppDialog open busy onClose={onClose}>content</AppDialog>);
  fireEvent.keyDown(screen.getByRole('dialog'), { key: 'Escape' });
  expect(onClose).not.toHaveBeenCalled();
});
