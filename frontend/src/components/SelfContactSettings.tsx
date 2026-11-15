import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Autocomplete,
  Box,
  Card,
  CardContent,
  CircularProgress,
  Divider,
  TextField,
  Typography,
} from '@mui/material';
import AccountCircleIcon from '@mui/icons-material/AccountCircle';
import { getCurrentUser } from '../api/admin';
import { Contact, getContact } from '../api/contacts';
import { setSelfContact } from '../api/users';
import { useContactSearch } from '../hooks/useContactSearch';
import { useSnackbar } from '../context/SnackbarContext';
import { ApiError } from '../api/client';
import { contactDisplayName } from '../utils/contactName';

const NAME_FIELDS = ['ID', 'firstname', 'lastname', 'nickname'];

/**
 * Settings card for choosing which contact is the user themself ("me").
 * Optional: leaving it empty is a valid state, and clearing it removes the mark.
 */
export default function SelfContactSettings() {
  const { t } = useTranslation();
  const { showSuccess, showError } = useSnackbar();
  const [selected, setSelected] = useState<Contact | null>(null);
  // Until the current mark is known the picker stays disabled, so it can't overwrite a mark it hasn't seen.
  const [loaded, setLoaded] = useState(false);
  const [saving, setSaving] = useState(false);
  const [open, setOpen] = useState(false);
  const { options, loading, search, setSearch } = useContactSearch(open);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const user = await getCurrentUser();
        const id = user.self_contact_id;
        // The mark is known even if its contact can't be read; the picker then shows empty
        // but stays usable, so the user can still choose another contact.
        const contact = id ? await getContact(id, NAME_FIELDS).catch(() => null) : null;
        if (!cancelled) {
          setSelected(contact);
          setLoaded(true);
        }
      } catch (err) {
        console.error('Failed to load the contact marked as you:', err);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  const handleChange = async (contact: Contact | null) => {
    if ((contact?.ID ?? null) === (selected?.ID ?? null)) return;
    const previous = selected;
    setSelected(contact);
    setSaving(true);
    try {
      await setSelfContact(contact ? contact.ID : null);
      showSuccess(contact ? t('settings.self.saved') : t('settings.self.cleared'));
    } catch (err) {
      setSelected(previous);
      showError(err instanceof ApiError ? err.getDisplayMessage() : t('settings.self.error'));
    } finally {
      setSaving(false);
    }
  };

  return (
    <Card sx={{ mb: 2 }}>
      <CardContent sx={{ py: 1.5, '&:last-child': { pb: 1.5 } }}>
        <Box sx={{ display: 'flex', alignItems: 'center', mb: 1 }}>
          <AccountCircleIcon sx={{ mr: 1, color: 'text.secondary', fontSize: 20 }} />
          <Typography variant="subtitle1" sx={{ fontWeight: 500 }}>
            {t('settings.self.title')}
          </Typography>
        </Box>
        <Divider sx={{ mb: 1.5 }} />

        <Autocomplete
          size="small"
          options={options}
          value={selected}
          open={open}
          onOpen={() => setOpen(true)}
          onClose={() => setOpen(false)}
          onChange={(_, value) => handleChange(value)}
          onInputChange={(_, value, reason) => {
            if (reason === 'input') setSearch(value);
          }}
          getOptionLabel={(option) => contactDisplayName(option)}
          isOptionEqualToValue={(option, value) => option.ID === value.ID}
          filterOptions={(x) => x} // The server filters.
          loading={loading}
          disabled={!loaded || saving}
          noOptionsText={search ? t('relationships.noContactsFound') : t('relationships.typeToSearch')}
          renderInput={(params) => (
            <TextField
              {...params}
              label={t('settings.self.label')}
              placeholder={t('settings.self.placeholder')}
              slotProps={{
                ...params.slotProps,
                input: {
                  ...params.slotProps.input,
                  endAdornment: (
                    <>
                      {loading || saving ? <CircularProgress color="inherit" size={18} /> : null}
                      {params.slotProps.input.endAdornment}
                    </>
                  ),
                },
              }}
            />
          )}
        />

        <Typography variant="caption" sx={{ color: 'text.secondary', mt: 1, display: 'block' }}>
          {t('settings.self.description')}
        </Typography>
      </CardContent>
    </Card>
  );
}
