import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  DialogTitle,
  DialogContent,
  DialogActions,
  TextField,
  Button,
  Box,
  Autocomplete,
  Chip,
  IconButton
} from '@mui/material';
import AppDialog from './AppDialog';
import { useConfirm } from './ConfirmDialog';
import { useContactSearch } from '../hooks/useContactSearch';
import DeleteIcon from '@mui/icons-material/Delete';
import { useDateFormat } from '../DateFormatProvider';
import { contactDisplayName } from '../utils/contactName';
import { ImageChange } from '../api/itemImages';
import ImageAttachmentField from './ImageAttachmentField';

type TimelineContact = { ID: number; firstname: string; lastname: string; nickname?: string };

export interface TimelineItemValues {
  noteContent?: string;
  noteDate?: string;
  activityTitle?: string;
  activityDescription?: string;
  activityLocation?: string;
  activityDate?: string;
  activityContacts?: TimelineContact[];
  /** The edited note or activity, and its current image. */
  itemId?: number;
  image?: string;
  imageChange?: ImageChange;
}

interface EditTimelineItemDialogProps {
  open: boolean;
  onClose: () => void;
  onSave: () => void | Promise<void>;
  onDelete: () => void;
  type: 'note' | 'activity';
  values: TimelineItemValues;
  onChange: (values: TimelineItemValues) => void;
  dateError?: string;
}

export default function EditTimelineItemDialog({
  open,
  onClose,
  onSave,
  onDelete,
  type,
  values,
  onChange,
  dateError
}: EditTimelineItemDialogProps) {
  const { t } = useTranslation();
  const { autoFormatDateInput, getDatePlaceholder } = useDateFormat();
  const { confirm, confirmDialog } = useConfirm();
  const participantSearch = useContactSearch(open && type === 'activity');
  // Selected participants stay in the list even when the current search does not match them.
  const selectedContacts = values.activityContacts || [];
  const participantOptions: TimelineContact[] = [
    ...selectedContacts,
    ...participantSearch.options.filter((option) => !selectedContacts.some((c) => c.ID === option.ID)),
  ];

  // A save in flight can't be repeated or dismissed; an empty required field says so
  const [saving, setSaving] = useState(false);
  const [attempted, setAttempted] = useState(false);
  useEffect(() => { if (!open) setAttempted(false); }, [open]);
  const missing = type === 'note' ? !values.noteContent?.trim() : !values.activityTitle?.trim();
  const showMissing = attempted && missing;
  const requiredText = type === 'note' ? t('noteDialog.required') : t('activityDialog.required');

  const handleSave = async () => {
    setAttempted(true);
    if (missing) return;
    setSaving(true);
    try {
      await onSave();
    } finally {
      setSaving(false);
    }
  };

  const handleDelete = async () => {
    if (await confirm({ message: t('contactDetail.confirmDelete'), confirmLabel: t('common.delete'), destructive: true })) {
      onDelete();
    }
  };

  return (
    <AppDialog open={open} onClose={onClose} busy={saving} maxWidth="md" fullWidth>
      <DialogTitle>
        {type === 'note' ? t('contactDetail.editNote') : t('contactDetail.editActivity')}
      </DialogTitle>
      <DialogContent>
        {type === 'note' ? (
          // Edit Note
          <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2, mt: 1 }}>
            <TextField
              fullWidth
              multiline
              rows={6}
              value={values.noteContent || ''}
              onChange={(e) => onChange({ ...values, noteContent: e.target.value })}
              label={t('noteDialog.content')}
              placeholder={t('noteDialog.contentPlaceholder')}
              error={showMissing}
              helperText={showMissing ? requiredText : undefined}
              autoFocus
            />
            <TextField
              fullWidth
              value={values.noteDate || ''}
              onChange={(e) => onChange({ ...values, noteDate: autoFormatDateInput(e.target.value, values.noteDate || '') })}
              label={t('noteDialog.date')}
              placeholder={getDatePlaceholder()}
              error={!!dateError}
              helperText={dateError}
            />
            <ImageAttachmentField
              type="notes"
              id={values.itemId}
              image={values.image}
              value={values.imageChange}
              onChange={(imageChange) => onChange({ ...values, imageChange })}
            />
          </Box>
        ) : (
          // Edit Activity
          <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2, mt: 1 }}>
            <TextField
              fullWidth
              value={values.activityTitle || ''}
              onChange={(e) => onChange({ ...values, activityTitle: e.target.value })}
              label={t('activityDialog.activityTitle')}
              error={showMissing}
              helperText={showMissing ? requiredText : undefined}
              autoFocus
            />
            <TextField
              fullWidth
              multiline
              rows={3}
              value={values.activityDescription || ''}
              onChange={(e) => onChange({ ...values, activityDescription: e.target.value })}
              label={t('activityDialog.description')}
            />
            <TextField
              fullWidth
              value={values.activityLocation || ''}
              onChange={(e) => onChange({ ...values, activityLocation: e.target.value })}
              label={t('activityDialog.location')}
            />
            <TextField
              fullWidth
              value={values.activityDate || ''}
              onChange={(e) => onChange({ ...values, activityDate: autoFormatDateInput(e.target.value, values.activityDate || '') })}
              label={t('activityDialog.date')}
              placeholder={getDatePlaceholder()}
              error={!!dateError}
              helperText={dateError}
            />
            <Autocomplete
              multiple
              options={participantOptions}
              filterOptions={(x) => x}
              loading={participantSearch.loading}
              inputValue={participantSearch.search}
              onInputChange={(_, value) => participantSearch.setSearch(value)}
              isOptionEqualToValue={(option, value) => option.ID === value.ID}
              getOptionLabel={(contact) => contactDisplayName(contact, { nickname: true })}
              value={selectedContacts}
              onChange={(_, newValue) => onChange({ ...values, activityContacts: newValue })}
              renderInput={(params) => (
                <TextField
                  {...params}
                  label={t('activityDialog.contacts')}
                  placeholder={t('activityDialog.selectContacts')}
                />
              )}
              renderValue={(value, getItemProps) =>
                value.map((contact, index) => (
                  <Chip
                    label={contactDisplayName(contact, { nickname: true })}
                    {...getItemProps({ index })}
                    size="small"
                    key={contact.ID}
                  />
                ))
              }
            />
            <ImageAttachmentField
              type="activities"
              id={values.itemId}
              image={values.image}
              value={values.imageChange}
              onChange={(imageChange) => onChange({ ...values, imageChange })}
            />
          </Box>
        )}
      </DialogContent>
      <DialogActions sx={{ justifyContent: 'space-between', px: 3, pb: 2 }}>
        <IconButton 
          color="error"
          onClick={handleDelete}
          disabled={saving}
          title={t('contactDetail.delete')}
          aria-label={t('contactDetail.delete')}
        >
          <DeleteIcon />
        </IconButton>
        <Box>
          <Button onClick={onClose} sx={{ mr: 1 }}>
            {t('contactDetail.cancel')}
          </Button>
          <Button onClick={handleSave} variant="contained" disabled={saving}>
            {t('contactDetail.save')}
          </Button>
        </Box>
      </DialogActions>
      {confirmDialog}
    </AppDialog>
  );
}
