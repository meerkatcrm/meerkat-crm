import { test, expect, vi, afterEach } from 'vitest';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { I18nextProvider } from 'react-i18next';
import i18n from '../i18n/config';
import ConfirmDialog, { useConfirm } from './ConfirmDialog';

afterEach(cleanup);

const wrap = (ui: React.ReactNode) => render(<I18nextProvider i18n={i18n}>{ui}</I18nextProvider>);

test('renders title, message and default button labels', () => {
  wrap(<ConfirmDialog open title="Title" message="Really?" onConfirm={vi.fn()} onCancel={vi.fn()} />);
  expect(screen.getByText('Title')).toBeInTheDocument();
  expect(screen.getByText('Really?')).toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Cancel' })).toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Continue' })).toBeInTheDocument();
});

test('confirm and cancel call their handlers; Esc cancels', () => {
  const onConfirm = vi.fn();
  const onCancel = vi.fn();
  wrap(<ConfirmDialog open message="Really?" confirmLabel="Delete" onConfirm={onConfirm} onCancel={onCancel} />);
  fireEvent.click(screen.getByRole('button', { name: 'Delete' }));
  expect(onConfirm).toHaveBeenCalledTimes(1);
  fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
  fireEvent.keyDown(screen.getByRole('dialog'), { key: 'Escape' });
  expect(onCancel).toHaveBeenCalledTimes(2);
});

test('busy disables both buttons and ignores Esc', () => {
  const onCancel = vi.fn();
  wrap(<ConfirmDialog open busy message="Really?" onConfirm={vi.fn()} onCancel={onCancel} />);
  expect(screen.getByRole('button', { name: 'Cancel' })).toBeDisabled();
  expect(screen.getByRole('button', { name: 'Continue' })).toBeDisabled();
  fireEvent.keyDown(screen.getByRole('dialog'), { key: 'Escape' });
  expect(onCancel).not.toHaveBeenCalled();
});

function Harness({ onResult }: { onResult: (confirmed: boolean) => void }) {
  const { confirm, confirmDialog } = useConfirm();
  return (
    <>
      <button onClick={async () => onResult(await confirm({ message: 'Sure?', confirmLabel: 'Yes' }))}>ask</button>
      {confirmDialog}
    </>
  );
}

test('useConfirm resolves true on confirm and false on cancel', async () => {
  const onResult = vi.fn();
  wrap(<Harness onResult={onResult} />);

  fireEvent.click(screen.getByText('ask'));
  expect(screen.getByText('Sure?')).toBeInTheDocument();
  await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Yes' })); });
  expect(onResult).toHaveBeenLastCalledWith(true);
  await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());

  fireEvent.click(screen.getByText('ask'));
  await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Cancel' })); });
  expect(onResult).toHaveBeenLastCalledWith(false);
});
