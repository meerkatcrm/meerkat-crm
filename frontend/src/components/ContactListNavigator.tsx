import { useEffect, useRef, useState } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { Box, Button, Typography } from '@mui/material';
import ChevronLeftIcon from '@mui/icons-material/ChevronLeft';
import ChevronRightIcon from '@mui/icons-material/ChevronRight';
import { getContacts } from '../api/contacts';
import { readContactListNav } from '../contactListNav';
import { useSnackbar } from '../context/SnackbarContext';
import { getErrorMessage } from '../utils/errorHandler';

// Previous / next buttons for stepping through the contact list the page was
// opened from. Renders nothing when the contact was opened any other way.
export default function ContactListNavigator() {
  const { t } = useTranslation();
  const location = useLocation();
  const navigate = useNavigate();
  const { showError } = useSnackbar();
  const [busy, setBusy] = useState(false);
  // A step that resolves after the user went elsewhere must not pull them back
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  const nav = readContactListNav(location.state);

  if (!nav || nav.total < 2) return null;

  const go = async (delta: number) => {
    const index = nav.index + delta;
    if (index < 0 || index >= nav.total) return;
    setBusy(true);
    try {
      // With one contact per page, page n is the contact at position n - 1.
      const result = await getContacts({ ...nav.params, page: index + 1, limit: 1 });
      if (!mounted.current) return;
      const next = result.contacts[0];
      if (!next) {
        // The list shrank since it was opened: there is nothing at that position any more
        showError(t('contactDetail.listNav.gone'));
        return;
      }
      navigate(`/contacts/${next.ID}`, {
        state: { listNav: { ...nav, index, total: result.total } },
      });
    } catch (err) {
      if (mounted.current) showError(getErrorMessage(err));
    } finally {
      if (mounted.current) setBusy(false);
    }
  };

  return (
    <Box
      component="nav"
      aria-label={t('contactDetail.listNav.label')}
      sx={{ maxWidth: 1200, mx: 'auto', mt: 1, px: 2, display: 'flex', alignItems: 'center', justifyContent: 'flex-end', gap: 1 }}
    >
      <Button
        size="small"
        startIcon={<ChevronLeftIcon />}
        disabled={busy || nav.index <= 0}
        onClick={() => go(-1)}
      >
        {t('contactDetail.listNav.previous')}
      </Button>
      <Typography variant="body2" color="text.secondary">
        {t('contactDetail.listNav.position', { position: nav.index + 1, total: nav.total })}
      </Typography>
      <Button
        size="small"
        endIcon={<ChevronRightIcon />}
        disabled={busy || nav.index >= nav.total - 1}
        onClick={() => go(1)}
      >
        {t('contactDetail.listNav.next')}
      </Button>
    </Box>
  );
}
