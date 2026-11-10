import { useState, useEffect } from 'react';
import {
  DialogTitle,
  DialogContent,
  DialogActions,
  TextField,
  Button,
  Box,
  Autocomplete,
  Chip,
  CircularProgress,
} from '@mui/material';
import AppDialog from './AppDialog';
import { useTranslation } from 'react-i18next';
import { Contact, getContact } from '../api/contacts';
import { useContactSearch } from '../hooks/useContactSearch';
import { useDateFormat } from '../DateFormatProvider';
import { todayISO } from '../utils/today';
import { contactDisplayName } from '../utils/contactName';

interface AddActivityDialogProps {
  open: boolean;
  onClose: () => void;
  onSave: (activity: {
    title: string;
    description: string;
    location: string;
    date: string;
    contact_ids: number[];
  }) => Promise<void>;
  preselectedContactId?: number;
}

export default function AddActivityDialog({
  open,
  onClose,
  onSave,
  preselectedContactId,
}: AddActivityDialogProps) {
  const { t } = useTranslation();
  const { formatDate, parseDateInput, autoFormatDateInput, getDatePlaceholder } = useDateFormat();
  const [title, setTitle] = useState('');
  const [description, setDescription] = useState('');
  const [location, setLocation] = useState('');
  const [date, setDate] = useState(formatDate(todayISO()));
  const [selectedContacts, setSelectedContacts] = useState<Contact[]>([]);
  const [error, setError] = useState('');
  const [saving, setSaving] = useState(false);
  // Saving before the preselected contact arrives would store the activity without it.
  const [loadingPreselected, setLoadingPreselected] = useState(false);
  const {
    options: contacts,
    loading,
    search: searchInput,
    setSearch: setSearchInput,
  } = useContactSearch(open);

  // Load preselected contact on open
  useEffect(() => {
    // The picker's options come from useContactSearch either way.
    if (!open || !preselectedContactId) return;

    // Fetched by id: looking for it in a page of the contact list misses
    // everyone beyond that page.
    let cancelled = false;
    const loadPreselected = async () => {
      setLoadingPreselected(true);
      try {
        const preselected = await getContact(preselectedContactId, ['ID', 'firstname', 'lastname', 'nickname']);
        if (!cancelled) {
          setSelectedContacts([preselected]);
        }
      } catch (err) {
        console.error('Failed to load preselected contact:', err);
      } finally {
        if (!cancelled) {
          setLoadingPreselected(false);
        }
      }
    };
    loadPreselected();
    return () => {
      cancelled = true;
      setLoadingPreselected(false);
    };
  }, [open, preselectedContactId]);

  const handleSave = async () => {
    if (!title.trim() || !date) {
      setError(t('activityDialog.required'));
      return;
    }

    const parsedDate = parseDateInput(date);
    if (!parsedDate) {
      setError(t('activityDialog.dateError'));
      return;
    }

    setSaving(true);
    try {
      await onSave({
        title,
        description,
        location,
        date: parsedDate,
        contact_ids: selectedContacts.map((c) => c.ID),
      });
      handleClose();
    } catch {
      setError('Failed to save activity');
    } finally {
      setSaving(false);
    }
  };

  const handleClose = () => {
    setTitle('');
    setDescription('');
    setLocation('');
    setDate(formatDate(todayISO()));
    setSelectedContacts([]);
    setSearchInput('');
    setError('');
    onClose();
  };

  const getContactLabel = (contact: Contact) => contactDisplayName(contact, { nickname: true });

  return (
    <AppDialog open={open} onClose={handleClose} maxWidth="sm" fullWidth>
      <DialogTitle>{t('activityDialog.title')}</DialogTitle>
      <DialogContent>
        <Box sx={{ pt: 1, display: 'flex', flexDirection: 'column', gap: 2 }}>
          <TextField
            label={t('activityDialog.activityTitle')}
            placeholder={t('activityDialog.titlePlaceholder')}
            value={title}
            onChange={(e) => {
              setTitle(e.target.value);
              setError('');
            }}
            error={!!error}
            helperText={error}
            fullWidth
            required
            autoFocus
          />
          
          <TextField
            label={t('activityDialog.description')}
            placeholder={t('activityDialog.descriptionPlaceholder')}
            multiline
            rows={3}
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            fullWidth
          />
          
          <TextField
            label={t('activityDialog.location')}
            placeholder={t('activityDialog.locationPlaceholder')}
            value={location}
            onChange={(e) => setLocation(e.target.value)}
            fullWidth
          />
          
          <TextField
            label={t('activityDialog.date')}
            placeholder={getDatePlaceholder()}
            value={date}
            onChange={(e) => setDate(autoFormatDateInput(e.target.value, date))}
            fullWidth
            required
          />
          
          <Autocomplete
            multiple
            options={contacts}
            getOptionLabel={getContactLabel}
            value={selectedContacts}
            onChange={(_, newValue) => setSelectedContacts(newValue)}
            onInputChange={(_, value) => setSearchInput(value)}
            inputValue={searchInput}
            loading={loading}
            filterOptions={(x) => x}
            isOptionEqualToValue={(option, value) => option.ID === value.ID}
            renderInput={(params) => (
              <TextField
                {...params}
                label={t('activityDialog.contacts')}
                placeholder={t('activityDialog.selectContacts')}
                slotProps={{
                  ...params.slotProps,
                  input: {
                    ...params.slotProps.input,
                    endAdornment: (
                      <>
                        {loading ? <CircularProgress color="inherit" size={20} /> : null}
                        {params.slotProps.input.endAdornment}
                      </>
                    ),
                  }
                }}
              />
            )}
            renderValue={(value, getItemProps) =>
              value.map((contact, index) => (
                <Chip
                  label={getContactLabel(contact)}
                  {...getItemProps({ index })}
                  key={contact.ID}
                />
              ))
            }
          />
        </Box>
      </DialogContent>
      <DialogActions>
        <Button onClick={handleClose} disabled={saving}>
          {t('activityDialog.cancel')}
        </Button>
        <Button onClick={handleSave} variant="contained" disabled={saving || loadingPreselected}>
          {t('activityDialog.save')}
        </Button>
      </DialogActions>
    </AppDialog>
  );
}
