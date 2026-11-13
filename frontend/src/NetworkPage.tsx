import { useState, useMemo, useEffect, useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import {
  Box,
  Card,
  Typography,
  CircularProgress,
  FormControlLabel,
  Switch,
  Select,
  MenuItem,
  FormControl,
  InputLabel,
  Alert,
  SelectChangeEvent,
  useMediaQuery,
  useTheme,
  Autocomplete,
  TextField,
  IconButton,
  Tooltip,
} from '@mui/material';
import AccountCircleIcon from '@mui/icons-material/AccountCircle';
import NetworkGraph from './components/NetworkGraph';
import NetworkLegend from './components/NetworkLegend';
import EditTimelineItemDialog from './components/EditTimelineItemDialog';
import { useGraph } from './hooks/useGraph';
import { GraphNode } from './types/graph';
import { Activity, getActivity, updateActivity, deleteActivity } from './api/activities';
import { applyImageChange, ImageChange } from './api/itemImages';
import { Contact } from './api/contacts';
import { getCurrentUser } from './api/admin';
import { NetworkFilters, loadNetworkFilters, saveNetworkFilters } from './networkFilters';
import { useDateFormat } from './DateFormatProvider';
import { useSnackbar } from './context/SnackbarContext';
import { handleError } from './utils/errorHandler';
import { now } from './utils/now';

export default function NetworkPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const theme = useTheme();
  const isMobile = useMediaQuery(theme.breakpoints.down('md'));
  const { data, loading, error, refetch } = useGraph();
  const { formatDate, parseDateInput } = useDateFormat();
  const { showError } = useSnackbar();

  const [filters, setFilters] = useState<NetworkFilters>(loadNetworkFilters);
  const { selectedCircle, showRelationships, showEndedRelationships, showActivities, showCircles } = filters;
  const [centeredNodeId, setCenteredNodeId] = useState<string | null>(() => {
    return localStorage.getItem('network-centered-node-id');
  });
  const [selfContactId, setSelfContactId] = useState<number | null>(null);
  const [editingActivity, setEditingActivity] = useState<Activity | null>(null);
  const [editValues, setEditValues] = useState<{
    activityTitle?: string;
    activityDescription?: string;
    activityLocation?: string;
    activityDate?: string;
    activityContacts?: Contact[];
    itemId?: number;
    image?: string;
    imageChange?: ImageChange;
  }>({});
  const [editDateError, setEditDateError] = useState('');

  const updateFilters = useCallback((patch: Partial<NetworkFilters>) => {
    setFilters(prev => ({ ...prev, ...patch }));
  }, []);

  useEffect(() => {
    saveNetworkFilters(filters);
  }, [filters]);

  // The contact marked as "me" gets a one-click way to center the graph on it.
  useEffect(() => {
    getCurrentUser()
      .then(user => setSelfContactId(user.self_contact_id ?? null))
      .catch(err => console.error('Error fetching current user:', err));
  }, []);

  useEffect(() => {
    if (centeredNodeId !== null) {
      localStorage.setItem('network-centered-node-id', centeredNodeId);
    } else {
      localStorage.removeItem('network-centered-node-id');
    }
  }, [centeredNodeId]);

  useEffect(() => {
    if (!data || !centeredNodeId) return;
    const nodeExists = data.nodes.some(n => n.id === centeredNodeId);
    if (!nodeExists) {
      setCenteredNodeId(null);
    }
  }, [data, centeredNodeId]);

  // Extract unique circles from contacts
  const circles = useMemo(() => {
    if (!data) return [];
    const allCircles = new Set<string>();
    data.nodes.forEach(n => {
      if (n.type === 'contact' && n.circles) {
        n.circles.forEach(c => allCircles.add(c));
      }
    });
    return Array.from(allCircles).sort();
  }, [data]);

  // Drop a persisted circle filter that no longer exists, so the graph doesn't
  // come up empty after the circle was renamed or removed.
  useEffect(() => {
    if (!data || !selectedCircle) return;
    if (!circles.includes(selectedCircle)) {
      updateFilters({ selectedCircle: '' });
    }
  }, [data, circles, selectedCircle, updateFilters]);

  // Contact nodes for the center-on-contact autocomplete
  const contactNodes = useMemo(() => {
    if (!data) return [];
    return data.nodes.filter(n => n.type === 'contact').sort((a, b) => a.label.localeCompare(b.label));
  }, [data]);

  const selfNode = useMemo(
    () => (selfContactId === null ? undefined : contactNodes.find(n => n.id === `c-${selfContactId}`)),
    [contactNodes, selfContactId]
  );

  // Handle node click - navigate to contact detail
  const handleNodeClick = (node: GraphNode) => {
    if (node.type === 'contact') {
      const contactId = node.id.replace('c-', '');
      navigate(`/contacts/${contactId}`);
    }
  };

  const handleActivityNodeClick = async (node: GraphNode) => {
    const id = parseInt(node.id.replace('a-', ''), 10);
    try {
      const activity = await getActivity(id);
      setEditingActivity(activity);
      setEditDateError('');
      setEditValues({
        activityTitle: activity.title || '',
        activityDescription: activity.description || '',
        activityLocation: activity.location || '',
        activityDate: activity.date ? formatDate(activity.date) : '',
        activityContacts: activity.contacts || [],
        itemId: activity.ID,
        image: activity.image,
      });
    } catch (err) {
      handleError(err, { operation: 'fetching activity' }, { showError });
    }
  };

  const handleActivityEditClose = () => {
    setEditingActivity(null);
    setEditValues({});
    setEditDateError('');
  };

  const handleActivitySave = async () => {
    if (!editingActivity || !editValues.activityTitle?.trim()) return;

    // The date field is free text in the user's display format.
    const parsedDate = editValues.activityDate ? parseDateInput(editValues.activityDate) : '';
    if (parsedDate === null) {
      setEditDateError(t('activityDialog.dateError'));
      return;
    }
    setEditDateError('');

    try {
      await updateActivity(editingActivity.ID, {
        title: editValues.activityTitle,
        description: editValues.activityDescription || '',
        location: editValues.activityLocation || '',
        date: parsedDate ? new Date(parsedDate).toISOString() : now().toISOString(),
        contact_ids: editValues.activityContacts?.map(c => c.ID) || [],
      });
      await applyImageChange('activities', editingActivity.ID, editValues.imageChange);
      handleActivityEditClose();
      await refetch();
    } catch (err) {
      handleError(err, { operation: 'updating activity' }, { showError });
    }
  };

  const handleActivityDelete = async () => {
    if (!editingActivity) return;
    try {
      await deleteActivity(editingActivity.ID);
      handleActivityEditClose();
      await refetch();
    } catch (err) {
      handleError(err, { operation: 'deleting activity' }, { showError });
    }
  };

  const handleCircleChange = (event: SelectChangeEvent<string>) => {
    updateFilters({ selectedCircle: event.target.value });
  };

  // Only the first load replaces the page: a reload after an edit keeps the graph
  // (and its layout and zoom) on screen.
  if (loading && !data) {
    return (
      <Box
        sx={{
          display: "flex",
          justifyContent: "center",
          alignItems: "center",
          minHeight: "60vh"
        }}>
        <CircularProgress />
      </Box>
    );
  }

  if (error) {
    return (
      <Box sx={{
        p: 2
      }}>
        <Alert severity="error">{error}</Alert>
      </Box>
    );
  }

  if (!data || data.nodes.length === 0) {
    return (
      <Box sx={{
        p: 2
      }}>
        <Alert severity="info">{t('network.noData')}</Alert>
      </Box>
    );
  }

  return (
    <Box sx={{ height: 'calc(100vh - 100px)', display: 'flex', flexDirection: 'column', mt: 2, p: 2 }}>
      <Typography variant="h5" gutterBottom sx={{ mb: 2 }}>
        {t('network.title')}
      </Typography>
      {/* Controls */}
      <Card
        sx={{
          p: 2,
          mb: 2,
          display: 'flex',
          gap: 2,
          flexWrap: 'wrap',
          alignItems: isMobile ? 'stretch' : 'center',
          flexDirection: isMobile ? 'column' : 'row',
          flexShrink: 0,
          overflow: 'visible',
        }}
      >
        {selfNode && (
          <Tooltip title={t('network.centerOnMe')}>
            <IconButton
              aria-label={t('network.centerOnMe')}
              aria-pressed={centeredNodeId === selfNode.id}
              color={centeredNodeId === selfNode.id ? 'primary' : 'default'}
              onClick={() => setCenteredNodeId(centeredNodeId === selfNode.id ? null : selfNode.id)}
            >
              <AccountCircleIcon />
            </IconButton>
          </Tooltip>
        )}

        <Autocomplete
          size="small"
          sx={{ minWidth: 200 }}
          options={contactNodes}
          getOptionLabel={(n) => n.label}
          value={contactNodes.find(n => n.id === centeredNodeId) ?? null}
          onChange={(_e, node) => setCenteredNodeId(node ? node.id : null)}
          renderInput={(params) => (
            <TextField {...params} label={t('network.filterByContact')} />
          )}
          clearOnEscape
        />

        <FormControl size="small" sx={{ minWidth: 150 }}>
          <InputLabel>{t('network.filterByCircle')}</InputLabel>
          <Select
            value={selectedCircle}
            onChange={handleCircleChange}
            label={t('network.filterByCircle')}
          >
            <MenuItem value="">{t('network.allCircles')}</MenuItem>
            {circles.map(c => (
              <MenuItem key={c} value={c}>{c}</MenuItem>
            ))}
          </Select>
        </FormControl>

        <FormControlLabel
          control={
            <Switch
              checked={showRelationships}
              onChange={(e) => updateFilters({ showRelationships: e.target.checked })}
              color="primary"
            />
          }
          label={t('network.showRelationships')}
        />

        <FormControlLabel
          control={
            <Switch
              checked={showEndedRelationships}
              onChange={(e) => updateFilters({ showEndedRelationships: e.target.checked })}
              color="primary"
              disabled={!showRelationships}
            />
          }
          label={t('network.showEndedRelationships')}
        />

        <FormControlLabel
          control={
            <Switch
              checked={showActivities}
              onChange={(e) => updateFilters({ showActivities: e.target.checked })}
              color="secondary"
            />
          }
          label={t('network.showActivities')}
        />

        <FormControlLabel
          control={
            <Switch
              checked={showCircles}
              onChange={(e) => updateFilters({ showCircles: e.target.checked })}
              color="warning"
            />
          }
          label={t('network.showCircles')}
        />

        {!isMobile && (
          <Box sx={{ flexBasis: '100%' }}>
            <NetworkLegend showCircles={showCircles} showActivities={showActivities} showRelationships={showRelationships} showEndedRelationships={showEndedRelationships} />
          </Box>
        )}
      </Card>

      {isMobile && (
        <Card sx={{ p: 1.5, mb: 2, flexShrink: 0 }}>
          <NetworkLegend showCircles={showCircles} showActivities={showActivities} showRelationships={showRelationships} showEndedRelationships={showEndedRelationships} />
        </Card>
      )}

      {/* Graph */}
      <Card sx={{ flex: 1, position: 'relative', overflow: 'hidden', minHeight: 400 }}>
        <NetworkGraph
          data={data}
          onNodeClick={handleNodeClick}
          onActivityClick={handleActivityNodeClick}
          selectedCircle={selectedCircle || undefined}
          showRelationships={showRelationships}
          showEndedRelationships={showEndedRelationships}
          showActivities={showActivities}
          showCircles={showCircles}
          centeredNodeId={centeredNodeId ?? undefined}
        />
      </Card>
      {editingActivity && (
        <EditTimelineItemDialog
          open
          onClose={handleActivityEditClose}
          onSave={handleActivitySave}
          onDelete={handleActivityDelete}
          type="activity"
          values={editValues}
          onChange={setEditValues}
          dateError={editDateError}
        />
      )}
    </Box>
  );
}
