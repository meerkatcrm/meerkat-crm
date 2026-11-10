import { useState, useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import ConfirmDialog from './ConfirmDialog';
import {
  Box,
  Card,
  CardContent,
  Typography,
  Divider,
  TextField,
  Button,
  Stack,
  Alert,
  IconButton,
  List,
  ListItem,
  ListItemText,
} from '@mui/material';
import TuneIcon from '@mui/icons-material/Tune';
import AddIcon from '@mui/icons-material/Add';
import DeleteIcon from '@mui/icons-material/Delete';
import ArrowUpwardIcon from '@mui/icons-material/ArrowUpward';
import ArrowDownwardIcon from '@mui/icons-material/ArrowDownward';
import { getCustomFieldNames, updateCustomFieldNames } from '../api/users';

export default function CustomFieldsSettings() {
  const { t } = useTranslation();
  const [fieldNames, setFieldNames] = useState<string[]>([]);
  const [newFieldName, setNewFieldName] = useState('');
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [fieldToDelete, setFieldToDelete] = useState<number | null>(null);

  useEffect(() => {
    const loadCustomFieldNames = async () => {
      try {
        setLoading(true);
        const names = await getCustomFieldNames();
        setFieldNames(names);
      } catch (err) {
        setError(err instanceof Error ? err.message : t('settings.customFields.loadError'));
      } finally {
        setLoading(false);
      }
    };
    loadCustomFieldNames();
  }, [t]);

  const saveFields = async (newNames: string[]) => {
    // Callers show newNames right away; this render's list is what to go back to if the save fails.
    const previousNames = fieldNames;
    try {
      setSaving(true);
      setError('');
      const savedNames = await updateCustomFieldNames(newNames);
      setFieldNames(savedNames);
    } catch (err) {
      setFieldNames(previousNames);
      setError(err instanceof Error ? err.message : t('settings.customFields.saveError'));
    } finally {
      setSaving(false);
    }
  };

  const handleAddField = async () => {
    const trimmedName = newFieldName.trim();
    if (!trimmedName) return;

    // Check for duplicates (case-insensitive)
    if (fieldNames.some(name => name.toLowerCase() === trimmedName.toLowerCase())) {
      setError(t('settings.customFields.duplicateError'));
      return;
    }

    const newNames = [...fieldNames, trimmedName];
    setFieldNames(newNames);
    setNewFieldName('');
    setError('');
    await saveFields(newNames);
  };

  const handleDeleteClick = (index: number) => {
    setFieldToDelete(index);
    setDeleteDialogOpen(true);
  };

  const handleDeleteConfirm = async () => {
    if (fieldToDelete !== null) {
      const newNames = fieldNames.filter((_, i) => i !== fieldToDelete);
      setFieldNames(newNames);
      setDeleteDialogOpen(false);
      setFieldToDelete(null);
      await saveFields(newNames);
    } else {
      setDeleteDialogOpen(false);
      setFieldToDelete(null);
    }
  };

  const handleMoveUp = async (index: number) => {
    if (index === 0) return;
    const newNames = [...fieldNames];
    [newNames[index - 1], newNames[index]] = [newNames[index], newNames[index - 1]];
    setFieldNames(newNames);
    await saveFields(newNames);
  };

  const handleMoveDown = async (index: number) => {
    if (index === fieldNames.length - 1) return;
    const newNames = [...fieldNames];
    [newNames[index], newNames[index + 1]] = [newNames[index + 1], newNames[index]];
    setFieldNames(newNames);
    await saveFields(newNames);
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter') {
      e.preventDefault();
      handleAddField();
    }
  };

  return (
    <>
      <Card sx={{ mb: 2 }}>
        <CardContent sx={{ py: 1.5, '&:last-child': { pb: 1.5 } }}>
          <Box sx={{ display: 'flex', alignItems: 'center', mb: 1 }}>
            <TuneIcon sx={{ mr: 1, color: 'text.secondary', fontSize: 20 }} />
            <Typography variant="subtitle1" sx={{ fontWeight: 500 }}>
              {t('settings.customFields.title')}
            </Typography>
          </Box>
          <Divider sx={{ mb: 1.5 }} />

          <Stack spacing={1.5}>
            <Typography variant="body2" sx={{
              color: "text.secondary"
            }}>
              {t('settings.customFields.description')}
            </Typography>

            {error && <Alert severity="error" sx={{ py: 0 }}>{error}</Alert>}

            {loading ? (
              <Typography variant="body2" sx={{
                color: "text.secondary"
              }}>
                {t('settings.customFields.loading')}
              </Typography>
            ) : (
              <>
                {fieldNames.length > 0 ? (
                  <List dense sx={{ py: 0 }}>
                    {fieldNames.map((name, index) => (
                      <ListItem
                        key={name}
                        // Keep the text clear of the absolutely positioned action buttons.
                        sx={{ pl: 0, pr: 15, overflowWrap: 'anywhere' }}
                        secondaryAction={
                          <>
                            <IconButton
                              size="small"
                              onClick={() => handleMoveUp(index)}
                              disabled={saving || index === 0}
                              aria-label={t('settings.customFields.moveUp')}
                            >
                              <ArrowUpwardIcon fontSize="small" />
                            </IconButton>
                            <IconButton
                              size="small"
                              onClick={() => handleMoveDown(index)}
                              disabled={saving || index === fieldNames.length - 1}
                              aria-label={t('settings.customFields.moveDown')}
                            >
                              <ArrowDownwardIcon fontSize="small" />
                            </IconButton>
                            <IconButton
                              size="small"
                              onClick={() => handleDeleteClick(index)}
                              disabled={saving}
                              aria-label={t('settings.customFields.delete')}
                              color="error"
                            >
                              <DeleteIcon fontSize="small" />
                            </IconButton>
                          </>
                        }
                      >
                        <ListItemText primary={name} />
                      </ListItem>
                    ))}
                  </List>
                ) : (
                  <Typography
                    variant="body2"
                    sx={{
                      color: "text.secondary",
                      fontStyle: 'italic'
                    }}>
                    {t('settings.customFields.noFields')}
                  </Typography>
                )}

                <Box sx={{ display: 'flex', gap: 1 }}>
                  <TextField
                    size="small"
                    placeholder={t('settings.customFields.newFieldPlaceholder')}
                    value={newFieldName}
                    onChange={(e) => setNewFieldName(e.target.value)}
                    onKeyDown={handleKeyDown}
                    disabled={saving}
                    sx={{ flexGrow: 1, minWidth: 0 }}
                    slotProps={{ htmlInput: { maxLength: 100 } }}
                  />
                  <Button
                    variant="outlined"
                    size="small"
                    startIcon={<AddIcon />}
                    onClick={handleAddField}
                    disabled={saving || !newFieldName.trim()}
                    sx={{ flexShrink: 0 }}
                  >
                    {t('settings.customFields.add')}
                  </Button>
                </Box>
              </>
            )}
          </Stack>
        </CardContent>
      </Card>

      <ConfirmDialog
        open={deleteDialogOpen}
        title={t('settings.customFields.deleteDialog.title')}
        message={t('settings.customFields.deleteDialog.message', {
          fieldName: fieldToDelete !== null ? fieldNames[fieldToDelete] : ''
        })}
        cancelLabel={t('settings.customFields.deleteDialog.cancel')}
        confirmLabel={t('settings.customFields.deleteDialog.confirm')}
        destructive
        onConfirm={handleDeleteConfirm}
        onCancel={() => setDeleteDialogOpen(false)}
      />
    </>
  );
}
