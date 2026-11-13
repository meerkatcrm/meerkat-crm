import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Alert,
  Autocomplete,
  Button,
  Chip,
  DialogActions,
  DialogContent,
  DialogTitle,
  FormControl,
  InputLabel,
  MenuItem,
  Select,
  Stack,
  TextField,
  Typography,
} from '@mui/material';
import AppDialog from './AppDialog';
import { bulkUpdateContacts, BulkContactField, BulkUpdateContactsInput } from '../api/contacts';
import { ContactFieldKey } from '../contactFields';
import { getErrorMessage } from '../utils/errorHandler';

interface BulkEditContactsDialogProps {
  open: boolean;
  contactIds: number[];
  availableCircles: string[];
  enabledFields: Set<ContactFieldKey>;
  onClose: () => void;
  onComplete: (updated: number) => void;
}

// The text fields bulk edit offers, each shown when the setting it belongs to is on.
const BULK_FIELDS: { key: BulkContactField; labelKey: string; enabledBy: ContactFieldKey }[] = [
  { key: 'city', labelKey: 'contacts.addressFields.city', enabledBy: 'addresses' },
  { key: 'organization', labelKey: 'contacts.organization', enabledBy: 'organization' },
  { key: 'department', labelKey: 'contacts.department', enabledBy: 'department' },
  { key: 'job_title', labelKey: 'contacts.jobTitle', enabledBy: 'job_title' },
  { key: 'role', labelKey: 'contacts.role', enabledBy: 'role' },
  { key: 'work_information', labelKey: 'contacts.workInformation', enabledBy: 'work_information' },
  { key: 'how_we_met', labelKey: 'contacts.howWeMet', enabledBy: 'how_we_met' },
  { key: 'food_preference', labelKey: 'contacts.foodPreference', enabledBy: 'food_preference' },
];

type ArchiveChoice = '' | 'archive' | 'unarchive';

// Adds a typed but not yet confirmed circle name, so it isn't lost when Apply is
// pressed without Enter first.
function withPending(values: string[], pending: string): string[] {
  const all = [...values, pending].map((v) => v.trim()).filter(Boolean);
  return all.filter((v, i) => all.indexOf(v) === i);
}

export default function BulkEditContactsDialog({
  open,
  contactIds,
  availableCircles,
  enabledFields,
  onClose,
  onComplete,
}: BulkEditContactsDialogProps) {
  const { t } = useTranslation();
  const [addCircles, setAddCircles] = useState<string[]>([]);
  const [addInput, setAddInput] = useState('');
  const [removeCircles, setRemoveCircles] = useState<string[]>([]);
  const [values, setValues] = useState<Partial<Record<BulkContactField, string>>>({});
  const [archive, setArchive] = useState<ArchiveChoice>('');
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const fields = BULK_FIELDS.filter((f) => enabledFields.has(f.enabledBy));
  const toAdd = withPending(addCircles, addInput);
  const overlap = toAdd.filter((c) => removeCircles.includes(c));
  const filledValues = Object.entries(values).filter(([, v]) => v && v.trim() !== '');
  const hasChange = toAdd.length > 0 || removeCircles.length > 0 || filledValues.length > 0 || archive !== '';

  const reset = () => {
    setAddCircles([]);
    setAddInput('');
    setRemoveCircles([]);
    setValues({});
    setArchive('');
    setError(null);
  };

  const handleClose = () => {
    if (saving) return;
    reset();
    onClose();
  };

  const handleApply = async () => {
    const input: BulkUpdateContactsInput = { contact_ids: contactIds };
    if (toAdd.length) input.add_circles = toAdd;
    if (removeCircles.length) input.remove_circles = removeCircles;
    for (const [key, value] of filledValues) {
      input[key as BulkContactField] = value!.trim();
    }
    if (archive) input.archived = archive === 'archive';

    setSaving(true);
    setError(null);
    try {
      const result = await bulkUpdateContacts(input);
      reset();
      onComplete(result.updated);
    } catch (err) {
      setError(getErrorMessage(err));
    } finally {
      setSaving(false);
    }
  };

  return (
    <AppDialog open={open} onClose={handleClose} busy={saving} maxWidth="sm" fullWidth>
      <DialogTitle>{t('contacts.bulkEdit.title', { count: contactIds.length })}</DialogTitle>
      <DialogContent>
        <Stack spacing={2} sx={{ mt: 1 }}>
          <Typography variant="body2" color="text.secondary">
            {t('contacts.bulkEdit.description')}
          </Typography>
          {error && <Alert severity="error">{error}</Alert>}

          <Autocomplete
            multiple
            freeSolo
            options={availableCircles}
            value={addCircles}
            inputValue={addInput}
            onInputChange={(_, value) => setAddInput(value)}
            onChange={(_, value) => setAddCircles(withPending(value as string[], ''))}
            renderValue={(selected, getItemProps) =>
              selected.map((option, index) => {
                const { key, ...itemProps } = getItemProps({ index });
                return <Chip key={key} label={option} size="small" {...itemProps} />;
              })
            }
            renderInput={(params) => (
              <TextField
                {...params}
                label={t('contacts.bulkEdit.addCircles')}
                helperText={t('contacts.bulkEdit.addCirclesHelp')}
              />
            )}
          />

          <Autocomplete
            multiple
            options={availableCircles}
            value={removeCircles}
            onChange={(_, value) => setRemoveCircles(value)}
            renderValue={(selected, getItemProps) =>
              selected.map((option, index) => {
                const { key, ...itemProps } = getItemProps({ index });
                return <Chip key={key} label={option} size="small" {...itemProps} />;
              })
            }
            renderInput={(params) => (
              <TextField {...params} label={t('contacts.bulkEdit.removeCircles')} />
            )}
          />
          {overlap.length > 0 && (
            <Alert severity="warning">{t('contacts.bulkEdit.circleConflict', { circles: overlap.join(', ') })}</Alert>
          )}

          {fields.length > 0 && (
            <>
              <Typography variant="subtitle2">{t('contacts.bulkEdit.setFields')}</Typography>
              {fields.map((field) => (
                <TextField
                  key={field.key}
                  label={t(field.labelKey)}
                  value={values[field.key] ?? ''}
                  onChange={(e) => setValues((prev) => ({ ...prev, [field.key]: e.target.value }))}
                  helperText={field.key === 'city' ? t('contacts.bulkEdit.cityHelp') : undefined}
                  size="small"
                  fullWidth
                />
              ))}
            </>
          )}

          <FormControl size="small" fullWidth>
            <InputLabel id="bulk-archive-label">{t('contacts.bulkEdit.archiveLabel')}</InputLabel>
            <Select
              labelId="bulk-archive-label"
              value={archive}
              label={t('contacts.bulkEdit.archiveLabel')}
              onChange={(e) => setArchive(e.target.value as ArchiveChoice)}
            >
              <MenuItem value="">{t('contacts.bulkEdit.noChange')}</MenuItem>
              <MenuItem value="archive">{t('contacts.bulkEdit.archive')}</MenuItem>
              <MenuItem value="unarchive">{t('contacts.bulkEdit.unarchive')}</MenuItem>
            </Select>
          </FormControl>
          {archive === 'archive' && (
            <Alert severity="info">{t('contacts.bulkEdit.archiveWarning')}</Alert>
          )}
        </Stack>
      </DialogContent>
      <DialogActions>
        <Button onClick={handleClose} disabled={saving}>
          {t('common.cancel')}
        </Button>
        <Button
          variant="contained"
          onClick={handleApply}
          disabled={saving || !hasChange || overlap.length > 0}
        >
          {t('contacts.bulkEdit.apply', { count: contactIds.length })}
        </Button>
      </DialogActions>
    </AppDialog>
  );
}
