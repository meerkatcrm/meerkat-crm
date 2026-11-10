import { test, expect, vi, afterEach } from 'vitest';
import { copyToClipboard } from './clipboard';

const original = Object.getOwnPropertyDescriptor(navigator, 'clipboard');

function setClipboard(value: unknown) {
  Object.defineProperty(navigator, 'clipboard', { value, configurable: true });
}

afterEach(() => {
  if (original) {
    Object.defineProperty(navigator, 'clipboard', original);
  } else {
    delete (navigator as { clipboard?: unknown }).clipboard;
  }
});

test('copies the text and reports success', async () => {
  const writeText = vi.fn().mockResolvedValue(undefined);
  setClipboard({ writeText });
  await expect(copyToClipboard('secret')).resolves.toBe(true);
  expect(writeText).toHaveBeenCalledWith('secret');
});

test('reports failure instead of throwing when the clipboard is unavailable (plain HTTP)', async () => {
  setClipboard(undefined);
  await expect(copyToClipboard('secret')).resolves.toBe(false);
});

test('reports failure when the browser refuses the write', async () => {
  setClipboard({ writeText: vi.fn().mockRejectedValue(new Error('denied')) });
  await expect(copyToClipboard('secret')).resolves.toBe(false);
});
