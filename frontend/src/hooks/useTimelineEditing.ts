import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useDateFormat } from '../DateFormatProvider';
import { updateNote, deleteNote, Note } from '../api/notes';
import { updateActivity, deleteActivity, Activity } from '../api/activities';
import { applyImageChange } from '../api/itemImages';
import { handleError, ErrorNotifier } from '../utils/errorHandler';
import type { TimelineItemValues } from '../components/EditTimelineItemDialog';

export function useTimelineEditing(
  contactId: number | undefined,
  onRefresh: () => Promise<void>,
  notifier?: ErrorNotifier
) {
  const { t } = useTranslation();
  const { formatDate, parseDateInput } = useDateFormat();
  const [editTimelineDateError, setEditTimelineDateError] = useState('');
  const [editingTimelineItem, setEditingTimelineItem] = useState<{ type: 'note' | 'activity'; id: number } | null>(null);
  const [editTimelineValues, setEditTimelineValues] = useState<TimelineItemValues>({});

  const handleStartEditTimelineItem = (type: 'note' | 'activity', item: Note | Activity) => {
    setEditingTimelineItem({ type, id: item.ID });
    setEditTimelineDateError('');

    if (type === 'note') {
      const note = item as Note;
      setEditTimelineValues({
        noteContent: note.content || '',
        noteDate: note.date ? formatDate(note.date) : '',
        itemId: note.ID,
        image: note.image
      });
    } else {
      const activity = item as Activity;

      setEditTimelineValues({
        activityTitle: activity.title || '',
        activityDescription: activity.description || '',
        activityLocation: activity.location || '',
        activityDate: activity.date ? formatDate(activity.date) : '',
        activityContacts: activity.contacts || [],
        itemId: activity.ID,
        image: activity.image
      });
    }
  };

  const handleCancelEditTimelineItem = () => {
    setEditingTimelineItem(null);
    setEditTimelineValues({});
    setEditTimelineDateError('');
  };

  // The date field is free text in the user's display format. Returns undefined
  // (and shows the field error) when it cannot be read as a date.
  const parseEditedDate = (input: string | undefined, errorKey: string): string | undefined => {
    const parsed = input ? parseDateInput(input) : '';
    if (parsed === null) {
      setEditTimelineDateError(t(errorKey));
      return undefined;
    }
    setEditTimelineDateError('');
    return parsed ? new Date(parsed).toISOString() : new Date().toISOString();
  };

  const handleUpdateNote = async (noteId: number) => {
    if (!editTimelineValues.noteContent?.trim()) return;

    const date = parseEditedDate(editTimelineValues.noteDate, 'noteDialog.dateError');
    if (!date) return;

    try {
      await updateNote(noteId, {
        content: editTimelineValues.noteContent,
        date,
        contact_id: contactId
      });
      await applyImageChange('notes', noteId, editTimelineValues.imageChange);
      await onRefresh();
      handleCancelEditTimelineItem();
    } catch (err) {
      handleError(err, { operation: 'updating note' }, notifier);
    }
  };

  const handleUpdateActivity = async (activityId: number) => {
    if (!editTimelineValues.activityTitle?.trim()) return;

    const date = parseEditedDate(editTimelineValues.activityDate, 'activityDialog.dateError');
    if (!date) return;

    try {
      await updateActivity(activityId, {
        title: editTimelineValues.activityTitle,
        description: editTimelineValues.activityDescription || '',
        location: editTimelineValues.activityLocation || '',
        date,
        contact_ids: editTimelineValues.activityContacts?.map(c => c.ID) || []
      });
      await applyImageChange('activities', activityId, editTimelineValues.imageChange);
      await onRefresh();
      handleCancelEditTimelineItem();
    } catch (err) {
      handleError(err, { operation: 'updating activity' }, notifier);
    }
  };

  const handleDeleteNote = async (noteId: number) => {
    try {
      await deleteNote(noteId);
      await onRefresh();
      handleCancelEditTimelineItem();
    } catch (err) {
      handleError(err, { operation: 'deleting note' }, notifier);
    }
  };

  const handleDeleteActivity = async (activityId: number) => {
    try {
      await deleteActivity(activityId);
      await onRefresh();
      handleCancelEditTimelineItem();
    } catch (err) {
      handleError(err, { operation: 'deleting activity' }, notifier);
    }
  };

  return {
    editingTimelineItem,
    editTimelineValues,
    editTimelineDateError,
    handleStartEditTimelineItem,
    handleCancelEditTimelineItem,
    handleUpdateNote,
    handleUpdateActivity,
    handleDeleteNote,
    handleDeleteActivity,
    setEditTimelineValues
  };
}
