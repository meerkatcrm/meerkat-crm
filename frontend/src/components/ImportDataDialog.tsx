import { useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Alert,
  Box,
  Button,
  CircularProgress,
  DialogActions,
  DialogContent,
  DialogTitle,
  Stack,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableRow,
  Typography,
} from '@mui/material';
import UploadFileIcon from '@mui/icons-material/UploadFile';
import AppDialog from './AppDialog';
import { importDataExport, type DataImportResult } from '../api/import';

const KINDS = ['contacts', 'relationships', 'activities', 'notes', 'reminders'] as const;
const MAX_SIZE = 10 * 1024 * 1024;

interface ImportDataDialogProps {
  open: boolean;
  onClose: () => void;
}

// Re-imports a CSV file from "Export Data": the file is first analyzed (a dry run on the
// server), then imported once the user has seen what will be added.
export default function ImportDataDialog({ open, onClose }: ImportDataDialogProps) {
  const { t } = useTranslation();
  const inputRef = useRef<HTMLInputElement>(null);
  const [file, setFile] = useState<File | null>(null);
  const [result, setResult] = useState<DataImportResult | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  const reset = () => {
    setFile(null);
    setResult(null);
    setError('');
  };

  const handleClose = () => {
    if (busy) return;
    reset();
    onClose();
  };

  const run = async (selected: File, dryRun: boolean) => {
    setError('');
    setBusy(true);
    try {
      setResult(await importDataExport(selected, dryRun));
    } catch (err) {
      setError(err instanceof Error && err.message ? err.message : t('settings.dataImport.error'));
    } finally {
      setBusy(false);
    }
  };

  const handleFileChange = (event: React.ChangeEvent<HTMLInputElement>) => {
    const selected = event.target.files?.[0];
    event.target.value = '';
    if (!selected) return;
    reset();
    if (!selected.name.toLowerCase().endsWith('.csv')) {
      setError(t('settings.dataImport.notCsv'));
      return;
    }
    if (selected.size > MAX_SIZE) {
      setError(t('settings.dataImport.tooLarge', { size: 10 }));
      return;
    }
    setFile(selected);
    void run(selected, true);
  };

  const done = result !== null && !result.dry_run;
  const nothingToImport =
    result !== null && KINDS.every((kind) => result[kind].created === 0) && result.custom_fields_added.length === 0;

  return (
    <AppDialog open={open} onClose={handleClose} busy={busy} maxWidth="sm" fullWidth>
      <DialogTitle>{t('settings.dataImport.title')}</DialogTitle>
      <DialogContent>
        <Stack spacing={2}>
          {!result && (
            <Typography variant="body2" sx={{ color: 'text.secondary' }}>
              {t('settings.dataImport.dialogIntro')}
            </Typography>
          )}

          <input ref={inputRef} type="file" accept=".csv" hidden onChange={handleFileChange} data-testid="data-import-file" />

          {!done && (
            <Box>
              <Button
                variant="outlined"
                size="small"
                startIcon={<UploadFileIcon />}
                onClick={() => inputRef.current?.click()}
                disabled={busy}
              >
                {file ? t('settings.dataImport.chooseAnother') : t('settings.dataImport.chooseFile')}
              </Button>
              {file && (
                <Typography variant="body2" component="span" sx={{ ml: 1.5, color: 'text.secondary' }}>
                  {file.name}
                </Typography>
              )}
            </Box>
          )}

          {busy && (
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
              <CircularProgress size={18} />
              <Typography variant="body2">
                {result ? t('settings.dataImport.importing') : t('settings.dataImport.analyzing')}
              </Typography>
            </Box>
          )}

          {error && <Alert severity="error">{error}</Alert>}

          {result && !busy && (
            <>
              {done ? (
                <Alert severity="success">{t('settings.dataImport.success')}</Alert>
              ) : (
                <Typography variant="body2">
                  {nothingToImport ? t('settings.dataImport.nothingToImport') : t('settings.dataImport.previewIntro')}
                </Typography>
              )}

              <Table size="small" aria-label={t('settings.dataImport.title')}>
                <TableHead>
                  <TableRow>
                    <TableCell />
                    <TableCell align="right">
                      {done ? t('settings.dataImport.columns.added') : t('settings.dataImport.columns.toAdd')}
                    </TableCell>
                    <TableCell align="right">{t('settings.dataImport.columns.existing')}</TableCell>
                    <TableCell align="right">{t('settings.dataImport.columns.skipped')}</TableCell>
                  </TableRow>
                </TableHead>
                <TableBody>
                  {KINDS.map((kind) => (
                    <TableRow key={kind}>
                      <TableCell component="th" scope="row">
                        {t(`settings.dataImport.kinds.${kind}`)}
                      </TableCell>
                      <TableCell align="right">{result[kind].created}</TableCell>
                      <TableCell align="right">{result[kind].existing}</TableCell>
                      <TableCell align="right">{result[kind].skipped}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>

              <Typography variant="caption" sx={{ color: 'text.secondary' }}>
                {t('settings.dataImport.existingHint')}
              </Typography>

              {result.custom_fields_added.length > 0 && (
                <Typography variant="body2">
                  {t('settings.dataImport.customFieldsAdded', { fields: result.custom_fields_added.join(', ') })}
                </Typography>
              )}

              {result.warnings.length > 0 && (
                <Alert severity="warning">
                  <Typography variant="body2" sx={{ fontWeight: 500, mb: 0.5 }}>
                    {t('settings.dataImport.warnings', { count: result.warnings.length + result.more_warnings })}
                  </Typography>
                  <Box component="ul" sx={{ m: 0, pl: 2.5 }}>
                    {result.warnings.map((warning, i) => (
                      <li key={i}>
                        <Typography variant="body2">{warning}</Typography>
                      </li>
                    ))}
                  </Box>
                  {result.more_warnings > 0 && (
                    <Typography variant="body2" sx={{ mt: 0.5 }}>
                      {t('settings.dataImport.moreWarnings', { count: result.more_warnings })}
                    </Typography>
                  )}
                </Alert>
              )}
            </>
          )}
        </Stack>
      </DialogContent>
      <DialogActions>
        <Button onClick={handleClose} disabled={busy}>
          {done ? t('common.close') : t('common.cancel')}
        </Button>
        {!done && (
          <Button
            variant="contained"
            onClick={() => file && run(file, false)}
            disabled={busy || !file || !result || nothingToImport}
          >
            {t('settings.dataImport.importButton')}
          </Button>
        )}
      </DialogActions>
    </AppDialog>
  );
}
