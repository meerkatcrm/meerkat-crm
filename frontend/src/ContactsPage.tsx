import { useEffect, useState, useMemo, useRef, useCallback } from 'react';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { useContacts } from './hooks/useContacts';
import { getCircles, getContacts } from './api/contacts';
import { getCurrentUser } from './api/admin';
import { resolveEnabledFields, ContactFieldKey } from './contactFields';
import AddContactDialog from './components/AddContactDialog';
import ImportContactsDialog from './components/ImportContactsDialog';
import BulkEditContactsDialog from './components/BulkEditContactsDialog';
import { ContactListNav, isNavigableSort } from './contactListNav';
import { useSnackbar } from './context/SnackbarContext';
import { getErrorMessage } from './utils/errorHandler';
import {
  Box,
  Card,
  Avatar,
  Typography,
  Chip,
  Stack,
  Select,
  MenuItem,
  FormControl,
  InputLabel,
  Pagination,
  Button,
  FormControlLabel,
  Switch,
  Checkbox
} from '@mui/material';
import ChecklistIcon from '@mui/icons-material/Checklist';
import EditIcon from '@mui/icons-material/Edit';
import PersonAddIcon from '@mui/icons-material/PersonAdd';
import FileUploadIcon from '@mui/icons-material/FileUpload';
import CallMergeIcon from '@mui/icons-material/CallMerge';
import { ContactListSkeleton } from './components/LoadingSkeletons';
import { contactDisplayName } from './utils/contactName';
import { thumbnailSrc } from './utils/thumbnail';

// Mirrors models.MaxBulkContacts on the server
const MAX_BULK_CONTACTS = 1000;

export default function ContactsPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const searchQuery = searchParams.get('search') || '';
  const page = parseInt(searchParams.get('page') || '1', 10);
  const [selectedCircle, setSelectedCircle] = useState('');
  const [circles, setCircles] = useState<string[]>([]);
  const [sortOption, setSortOption] = useState(() => {
    return localStorage.getItem('contacts-sort-option') || 'id-desc';
  });
  const [addDialogOpen, setAddDialogOpen] = useState(false);
  const [importDialogOpen, setImportDialogOpen] = useState(false);
  const [customFieldNames, setCustomFieldNames] = useState<string[]>([]);
  const [enabledFields, setEnabledFields] = useState<Set<ContactFieldKey>>(() => resolveEnabledFields(null));
  const [showArchived, setShowArchived] = useState(false);
  const [selectMode, setSelectMode] = useState(false);
  const [selectedIds, setSelectedIds] = useState<Set<number>>(() => new Set());
  const [selectingAll, setSelectingAll] = useState(false);
  const [bulkEditOpen, setBulkEditOpen] = useState(false);
  const { showError, showSuccess, showInfo } = useSnackbar();
  const pageSize = 10;

  // While searching, the default order is the best match first (the server ranks when no
  // sort is sent). Another order can be picked; it holds for that search only.
  const trimmedQuery = searchQuery.trim();
  const searching = trimmedQuery !== '';
  const [searchSort, setSearchSort] = useState({ query: trimmedQuery, option: 'relevance' });
  // Any change of the search, even back to an earlier term, starts at Best match again
  if (searchSort.query !== trimmedQuery) {
    setSearchSort({ query: trimmedQuery, option: 'relevance' });
  }
  const searchSortOption = searchSort.query === trimmedQuery ? searchSort.option : 'relevance';
  const activeSort = searching ? searchSortOption : sortOption;
  const [sortField, sortOrder] = activeSort.split('-');

  // Persist sort option to localStorage
  useEffect(() => {
    localStorage.setItem('contacts-sort-option', sortOption);
  }, [sortOption]);

  // Helper to update page in URL
  const setPage = useCallback((newPage: number) => {
    setSearchParams(prev => {
      const params = new URLSearchParams(prev);
      if (newPage === 1) {
        params.delete('page');
      } else {
        params.set('page', String(newPage));
      }
      return params;
    });
  }, [setSearchParams]);

  // Memoize params to prevent infinite re-renders
  const contactParams = useMemo(() => ({
    page,
    limit: pageSize,
    search: searchQuery,
    circle: selectedCircle,
    sort: sortField === 'relevance' ? undefined : sortField,
    order: sortField === 'relevance' ? undefined : sortOrder,
    includeArchived: showArchived,
  }), [page, searchQuery, selectedCircle, sortField, sortOrder, showArchived]);

  // The list's filters and order without paging: what "select all" and the detail
  // page's previous / next step through.
  const listParams = useMemo(() => {
    const { page: _page, limit: _limit, ...rest } = contactParams;
    return rest;
  }, [contactParams]);

  // The list showing now, for async work that must not apply to a list changed meanwhile
  const listParamsRef = useRef(listParams);
  listParamsRef.current = listParams;

  // Use custom hook for fetching contacts
  const { contacts, total: totalContacts, loading, refetch } = useContacts(contactParams);

  // Fetch circles for filter and custom field names
  useEffect(() => {
    const fetchData = async () => {
      try {
        const [circlesData, user] = await Promise.all([
          getCircles(),
          getCurrentUser()
        ]);
        setCircles(Array.isArray(circlesData) ? circlesData : []);
        setCustomFieldNames(user.custom_field_names ?? []);
        setEnabledFields(resolveEnabledFields(user.enabled_contact_fields));
      } catch (err) {
        console.error('Error fetching circles or custom field names:', err);
      }
    };
    fetchData();
  }, []);

  // Reset to page 1 when search, filter, sort or the archived toggle changes (but not on initial mount)
  const prevFiltersRef = useRef({ searchQuery, selectedCircle, activeSort, showArchived });
  useEffect(() => {
    const prev = prevFiltersRef.current;
    if (
      prev.searchQuery !== searchQuery ||
      prev.selectedCircle !== selectedCircle ||
      prev.activeSort !== activeSort ||
      prev.showArchived !== showArchived
    ) {
      setPage(1);
      // A selection is made within one list: other filters would hide what it holds
      setSelectedIds(new Set());
      prevFiltersRef.current = { searchQuery, selectedCircle, activeSort, showArchived };
    }
  }, [searchQuery, selectedCircle, activeSort, showArchived, setPage]);

  // Filter contacts by selected circle
  // With backend pagination, contacts are already filtered
  const filteredContacts = contacts;

  const handleContactAdded = (contactId: number) => {
    navigate(`/contacts/${contactId}`);
  };

  const refreshCircles = async () => {
    try {
      const data = await getCircles();
      setCircles(Array.isArray(data) ? data : []);
    } catch (err) {
      console.error('Error fetching circles:', err);
    }
  };

  const handleImportComplete = async () => {
    await refetch();
    await refreshCircles();
  };

  const toggleSelected = (id: number) => {
    setSelectedIds(prev => {
      const next = new Set(prev);
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
      }
      return next;
    });
  };

  const pageIds = filteredContacts.map(c => c.ID);
  const pageAllSelected = pageIds.length > 0 && pageIds.every(id => selectedIds.has(id));
  const pageSomeSelected = pageIds.some(id => selectedIds.has(id));

  const togglePage = () => {
    setSelectedIds(prev => {
      const next = new Set(prev);
      pageIds.forEach(id => (pageAllSelected ? next.delete(id) : next.add(id)));
      return next;
    });
  };

  // Collects the IDs of every contact matching the current filters, a page of the
  // largest size the server allows at a time.
  const selectAllMatching = async () => {
    const forList = listParams;
    setSelectingAll(true);
    try {
      const ids = new Set<number>();
      const limit = 100;
      for (let p = 1; ids.size < MAX_BULK_CONTACTS; p++) {
        // A fixed order, whatever the list shows: pages of a random one would overlap
        const result = await getContacts({ ...listParams, sort: 'id', order: 'desc', page: p, limit });
        result.contacts.forEach(c => ids.add(c.ID));
        if (result.contacts.length < limit || p * limit >= result.total) break;
      }
      // The filters changed while the IDs were collected: they belong to another list
      if (listParamsRef.current !== forList) return;
      setSelectedIds(ids);
    } catch (err) {
      showError(getErrorMessage(err));
    } finally {
      setSelectingAll(false);
    }
  };

  const exitSelectMode = () => {
    setSelectMode(false);
    setSelectedIds(new Set());
  };

  const handleBulkEditComplete = async (updated: number) => {
    setBulkEditOpen(false);
    setSelectedIds(new Set());
    if (updated > 0) {
      showSuccess(t('contacts.bulkEdit.success', { count: updated }));
    } else {
      showInfo(t('contacts.bulkEdit.noneChanged'));
    }
    await refetch();
    await refreshCircles();
  };

  // Previous / next on the detail page need a stable order; a random one isn't.
  const listNavFor = (index: number): ContactListNav | undefined =>
    isNavigableSort(listParams.sort)
      ? { params: listParams, index: (page - 1) * pageSize + index, total: totalContacts }
      : undefined;
  
  return (
    <Box sx={{ maxWidth: 1200, mx: 'auto', mt: 2, p: 2 }}>
      <Typography variant="h5" gutterBottom sx={{ mb: 2 }}>
        {t('contacts.title')}
      </Typography>
      <Stack
        direction={{ xs: 'column', sm: 'row' }}
        spacing={1.5}
        useFlexGap
        sx={{
          mb: 2,
          alignItems: "center",
          flexWrap: "wrap"
        }}>
        <FormControl sx={{ minWidth: 180 }} size="small">
          <InputLabel id="circle-select-label">{t('contacts.filterByCircle')}</InputLabel>
          <Select
            labelId="circle-select-label"
            value={selectedCircle}
            label={t('contacts.filterByCircle')}
            onChange={e => setSelectedCircle(e.target.value)}
          >
            <MenuItem value="">{t('contacts.allCircles')}</MenuItem>
            {circles.map(circle => (
              <MenuItem key={circle} value={circle}>{circle}</MenuItem>
            ))}
          </Select>
        </FormControl>
        <FormControl sx={{ minWidth: 150 }} size="small">
          <InputLabel id="sort-select-label">{t('contacts.sortBy')}</InputLabel>
          <Select
            labelId="sort-select-label"
            value={activeSort}
            label={t('contacts.sortBy')}
            onChange={e => (searching ? setSearchSort({ query: trimmedQuery, option: e.target.value }) : setSortOption(e.target.value))}
          >
            {searching && <MenuItem value="relevance">{t('contacts.sort.relevance')}</MenuItem>}
            <MenuItem value="id-desc">{t('contacts.sort.recentlyAdded')}</MenuItem>
            <MenuItem value="id-asc">{t('contacts.sort.oldestFirst')}</MenuItem>
            <MenuItem value="firstname-asc">{t('contacts.sort.nameAZ')}</MenuItem>
            <MenuItem value="firstname-desc">{t('contacts.sort.nameZA')}</MenuItem>
            <MenuItem value="random-asc">{t('contacts.sort.random')}</MenuItem>
          </Select>
        </FormControl>
        <FormControlLabel
          control={
            <Switch
              checked={showArchived}
              onChange={(e) => setShowArchived(e.target.checked)}
              size="small"
            />
          }
          label={t('contacts.showArchived')}
          sx={{ m: 0, whiteSpace: 'nowrap' }}
        />
        <Stack
          direction={{ xs: 'column', sm: 'row' }}
          spacing={1.5}
          useFlexGap
          sx={{ alignItems: 'center', flexWrap: 'wrap' }}
        >
          <Button
            variant={selectMode ? 'contained' : 'outlined'}
            startIcon={<ChecklistIcon />}
            onClick={() => (selectMode ? exitSelectMode() : setSelectMode(true))}
            sx={{ whiteSpace: 'nowrap' }}
          >
            {selectMode ? t('contacts.select.done') : t('contacts.select.button')}
          </Button>
          <Button
            variant="outlined"
            startIcon={<CallMergeIcon />}
            onClick={() => navigate('/contacts/duplicates')}
            sx={{ whiteSpace: 'nowrap' }}
          >
            {t('contacts.duplicates.button')}
          </Button>
          <Button
            variant="outlined"
            startIcon={<FileUploadIcon />}
            onClick={() => setImportDialogOpen(true)}
            sx={{ whiteSpace: 'nowrap' }}
          >
            {t('contacts.import.button', 'Import')}
          </Button>
          <Button
            variant="outlined"
            startIcon={<PersonAddIcon />}
            onClick={() => setAddDialogOpen(true)}
            sx={{ whiteSpace: 'nowrap' }}
          >
            {t('contacts.add.button')}
          </Button>
        </Stack>
      </Stack>
      {(totalContacts > 0 || searchQuery || selectedCircle) && (
        <Box sx={{ mb: 2, p: 1.5, bgcolor: 'action.hover', borderRadius: 1, display: 'flex', alignItems: 'center', gap: 1, flexWrap: 'wrap' }}>
          <Typography variant="body2" sx={{ flexGrow: 1 }}>
            {searchQuery && selectedCircle
              ? t('contacts.filteredBySearchAndCircle', { search: searchQuery, circle: selectedCircle, count: totalContacts })
              : searchQuery
                ? t('contacts.filteredBySearch', { search: searchQuery, count: totalContacts })
                : selectedCircle
                  ? t('contacts.filteredMessage', { count: filteredContacts.length, total: totalContacts, circle: selectedCircle })
                  : t('contacts.totalContacts', { count: totalContacts })
            }
          </Typography>
          {searchQuery && (
            <Chip 
              label={`"${searchQuery}"`} 
              size="small" 
              onDelete={() => navigate('/contacts')} 
            />
          )}
          {selectedCircle && (
            <Chip 
              label={selectedCircle} 
              size="small" 
              onDelete={() => { setSelectedCircle(''); setPage(1); }} 
            />
          )}
        </Box>
      )}
      {selectMode && (
        <Box
          sx={{
            mb: 2,
            p: 1,
            border: 1,
            borderColor: 'primary.main',
            borderRadius: 1,
            display: 'flex',
            alignItems: 'center',
            gap: 1,
            flexWrap: 'wrap',
          }}
        >
          <Checkbox
            checked={pageAllSelected}
            indeterminate={!pageAllSelected && pageSomeSelected}
            onChange={togglePage}
            disabled={pageIds.length === 0}
            slotProps={{ input: { 'aria-label': t('contacts.select.page') } }}
          />
          <Typography variant="body2" sx={{ flexGrow: 1 }}>
            {t('contacts.select.count', { count: selectedIds.size })}
          </Typography>
          {totalContacts > selectedIds.size && totalContacts <= MAX_BULK_CONTACTS && (
            <Button size="small" onClick={selectAllMatching} disabled={selectingAll}>
              {t('contacts.select.all', { count: totalContacts })}
            </Button>
          )}
          {selectedIds.size > 0 && (
            <Button size="small" onClick={() => setSelectedIds(new Set())}>
              {t('contacts.select.clear')}
            </Button>
          )}
          <Button
            size="small"
            variant="contained"
            startIcon={<EditIcon />}
            disabled={selectedIds.size === 0 || selectedIds.size > MAX_BULK_CONTACTS}
            onClick={() => setBulkEditOpen(true)}
          >
            {t('contacts.select.edit')}
          </Button>
        </Box>
      )}
      {loading ? (
        <ContactListSkeleton count={10} />
      ) : (
        <>
          <Stack spacing={2}>
            {filteredContacts.map((contact, index) => (
              <Card
                key={contact.ID}
                {...(selectMode
                  ? {
                      component: 'div' as const,
                      role: 'checkbox',
                      'aria-checked': selectedIds.has(contact.ID),
                      tabIndex: 0,
                      onClick: () => toggleSelected(contact.ID),
                      onKeyDown: (e: React.KeyboardEvent) => {
                        if (e.key === ' ' || e.key === 'Enter') {
                          e.preventDefault();
                          toggleSelected(contact.ID);
                        }
                      },
                    }
                  : {
                      component: Link,
                      to: `/contacts/${contact.ID}`,
                      state: { listNav: listNavFor(index) },
                    })}
                sx={{
                  display: 'flex',
                  alignItems: 'center',
                  p: 1.5,
                  cursor: 'pointer',
                  textDecoration: 'none',
                  color: 'inherit',
                  bgcolor: contact.archived ? 'action.disabledBackground' : undefined,
                  '&:hover': {
                    bgcolor: 'action.hover'
                  }
                }}
              >
                {selectMode && (
                  <Checkbox
                    checked={selectedIds.has(contact.ID)}
                    tabIndex={-1}
                    sx={{ mr: 0.5, pointerEvents: 'none' }}
                    slotProps={{ input: { 'aria-hidden': true } }}
                  />
                )}
                <Avatar src={thumbnailSrc(contact.photo_thumbnail)} sx={{ width: 48, height: 48, mr: 1.5, bgcolor: 'primary.main' }}>
                  {contact.firstname.charAt(0)}
                </Avatar>
                <Box sx={{ flex: 1 }}>
                  <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                    <Typography variant="body1" sx={{ fontWeight: 500 }}>
                      {contactDisplayName(contact, { nickname: true })}
                    </Typography>
                    {contact.archived && (
                      <Chip
                        label={t('contacts.archived')}
                        size="small"
                        color="default"
                        sx={{ height: 20, fontSize: '0.7rem' }}
                      />
                    )}
                  </Box>
                  <Stack
                    direction="row"
                    spacing={0.5}
                    sx={{
                      mt: 0.5,
                      flexWrap: "wrap",
                      gap: 0.5
                    }}>
                    {contact.circles && contact.circles.filter((circle, idx, arr) => arr.indexOf(circle) === idx).map((circle: string) => (
                      <Chip
                        key={`${contact.ID}-${circle}`}
                        label={circle}
                        size="small"
                        variant="outlined"
                        clickable={!selectMode}
                        onClick={selectMode ? undefined : (e) => { e.preventDefault(); e.stopPropagation(); setSelectedCircle(circle); setPage(1); }}
                        sx={{ height: 20, fontSize: '0.75rem' }}
                      />
                    ))}
                  </Stack>
                </Box>
              </Card>
            ))}
          </Stack>
          {totalContacts > 0 && (
            <Box sx={{ display: 'flex', justifyContent: 'center', mt: 2 }}>
              <Pagination
                count={Math.max(1, Math.ceil(totalContacts / pageSize))}
                page={page}
                onChange={(_, value) => setPage(value)}
                color="primary"
                size="large"
              />
            </Box>
          )}
        </>
      )}
      <AddContactDialog
        open={addDialogOpen}
        onClose={() => setAddDialogOpen(false)}
        onContactAdded={handleContactAdded}
        availableCircles={circles}
        customFieldNames={customFieldNames}
        enabledFields={enabledFields}
      />
      <BulkEditContactsDialog
        open={bulkEditOpen}
        contactIds={[...selectedIds]}
        availableCircles={circles}
        enabledFields={enabledFields}
        onClose={() => setBulkEditOpen(false)}
        onComplete={handleBulkEditComplete}
      />
      <ImportContactsDialog
        open={importDialogOpen}
        onClose={() => setImportDialogOpen(false)}
        onImportComplete={handleImportComplete}
      />
    </Box>
  );
}
