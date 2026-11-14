import { Alert, Button } from '@mui/material';
import { useTranslation } from 'react-i18next';

/** A failed load: say so, and offer another try instead of showing an empty list. */
export default function LoadError({ message, onRetry }: { message: string; onRetry: () => void }) {
  const { t } = useTranslation();
  return (
    <Alert
      severity="error"
      action={<Button color="inherit" size="small" onClick={onRetry}>{t('common.retry')}</Button>}
    >
      {message}
    </Alert>
  );
}
