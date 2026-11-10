import { ReactNode, useCallback, useRef, useState } from 'react';
import {
  Button,
  Dialog,
  DialogActions,
  DialogContent,
  DialogContentText,
  DialogTitle,
} from '@mui/material';
import { useTranslation } from 'react-i18next';

export interface ConfirmDialogProps {
  open: boolean;
  title?: ReactNode;
  message: ReactNode;
  // Defaults to common.continue / common.cancel.
  confirmLabel?: string;
  cancelLabel?: string;
  // Destructive confirmations render the confirm button in the error colour.
  destructive?: boolean;
  // While true (the confirmed action is in flight) the dialog can neither be
  // confirmed again nor dismissed.
  busy?: boolean;
  onConfirm: () => void;
  onCancel: () => void;
}

export default function ConfirmDialog({
  open,
  title,
  message,
  confirmLabel,
  cancelLabel,
  destructive = false,
  busy = false,
  onConfirm,
  onCancel,
}: ConfirmDialogProps) {
  const { t } = useTranslation();

  const handleCancel = () => {
    if (!busy) onCancel();
  };

  return (
    <Dialog open={open} onClose={handleCancel}>
      {title && <DialogTitle>{title}</DialogTitle>}
      <DialogContent>
        <DialogContentText sx={title ? undefined : { pt: 1 }}>{message}</DialogContentText>
      </DialogContent>
      <DialogActions>
        <Button onClick={handleCancel} disabled={busy}>
          {cancelLabel ?? t('common.cancel')}
        </Button>
        <Button
          onClick={onConfirm}
          color={destructive ? 'error' : 'primary'}
          variant="contained"
          disabled={busy}
          autoFocus
        >
          {confirmLabel ?? t('common.continue')}
        </Button>
      </DialogActions>
    </Dialog>
  );
}

export type ConfirmOptions = Pick<
  ConfirmDialogProps,
  'title' | 'message' | 'confirmLabel' | 'cancelLabel' | 'destructive'
>;

/**
 * Promise-based replacement for window.confirm. Render `confirmDialog` once in
 * the component and `await confirm({ message })`; it resolves true on confirm
 * and false on cancel, Esc or backdrop click.
 */
export function useConfirm() {
  const [open, setOpen] = useState(false);
  // Kept after closing so the content does not vanish during the exit transition.
  const [options, setOptions] = useState<ConfirmOptions>({ message: '' });
  const resolver = useRef<((confirmed: boolean) => void) | null>(null);

  const confirm = useCallback((next: ConfirmOptions) => {
    // A second request while one is pending cancels the first.
    resolver.current?.(false);
    setOptions(next);
    setOpen(true);
    return new Promise<boolean>((resolve) => {
      resolver.current = resolve;
    });
  }, []);

  const settle = (confirmed: boolean) => {
    resolver.current?.(confirmed);
    resolver.current = null;
    setOpen(false);
  };

  const confirmDialog = (
    <ConfirmDialog
      {...options}
      open={open}
      onConfirm={() => settle(true)}
      onCancel={() => settle(false)}
    />
  );

  return { confirm, confirmDialog };
}
