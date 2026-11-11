import { useState } from 'react';
import { createNote } from '../api/notes';
import { createActivity } from '../api/activities';
import { uploadItemImage } from '../api/itemImages';
import { handleError, ErrorNotifier, getErrorMessage } from '../utils/errorHandler';

export function useContactDialogs(
  contactId: string | undefined,
  onRefresh: () => Promise<void>,
  notifier?: ErrorNotifier
) {
  const [noteDialogOpen, setNoteDialogOpen] = useState(false);
  const [activityDialogOpen, setActivityDialogOpen] = useState(false);

  const handleSaveNote = async (content: string, date: string, image?: File) => {
    if (!contactId) return;

    try {
      const note = await createNote(contactId, {
        content,
        date: new Date(date).toISOString(),
        contact_id: parseInt(contactId)
      });
      if (image) await uploadItemImage('notes', note.ID, image);
      await onRefresh();
    } catch (err) {
      handleError(err, { operation: 'saving note' }, notifier);
      throw new Error(getErrorMessage(err));
    }
  };

  const handleSaveActivity = async (activity: {
    title: string;
    description: string;
    location: string;
    date: string;
    contact_ids: number[];
  }, image?: File) => {
    try {
      const created = await createActivity({
        title: activity.title,
        description: activity.description,
        location: activity.location,
        date: new Date(activity.date).toISOString(),
        contact_ids: activity.contact_ids
      });
      if (image) await uploadItemImage('activities', created.ID, image);
      await onRefresh();
    } catch (err) {
      handleError(err, { operation: 'saving activity' }, notifier);
      throw new Error(getErrorMessage(err));
    }
  };

  return {
    noteDialogOpen,
    activityDialogOpen,
    setNoteDialogOpen,
    setActivityDialogOpen,
    handleSaveNote,
    handleSaveActivity
  };
}
