import { test, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import '../i18n/config';
import { DateFormatProvider } from '../DateFormatProvider';
import { useTimelineEditing } from './useTimelineEditing';
import * as contactsApi from '../api/contacts';
import * as notesApi from '../api/notes';
import * as activitiesApi from '../api/activities';
import * as itemImagesApi from '../api/itemImages';
import type { Note } from '../api/notes';
import type { Activity } from '../api/activities';

vi.mock('../api/contacts');
vi.mock('../api/notes');
vi.mock('../api/activities');
vi.mock('../api/itemImages');

const contacts = vi.mocked(contactsApi);
const notes = vi.mocked(notesApi);
const activities = vi.mocked(activitiesApi);

const note: Note = { ID: 1, content: 'Hello', date: '2026-03-15T00:00:00Z', CreatedAt: '', UpdatedAt: '' };
const person = { ID: 9, firstname: 'Anna', lastname: 'Schmidt' };
const activity: Activity = {
  ID: 2,
  title: 'Lunch',
  description: 'Pizza',
  location: 'Berlin',
  date: '2026-04-01T00:00:00Z',
  CreatedAt: '',
  UpdatedAt: '',
  contacts: [person],
};

const quietLogs = () => vi.spyOn(console, 'error').mockImplementation(() => {});

function setup() {
  const onRefresh = vi.fn().mockResolvedValue(undefined);
  const showError = vi.fn();
  // The default date format is eu (DD.MM.YYYY), the one the edit dialog's free-text field then uses.
  const { result } = renderHook(() => useTimelineEditing(5, onRefresh, { showError }), { wrapper: DateFormatProvider });
  return { result, onRefresh, showError };
}

beforeEach(() => {
  localStorage.clear();
  vi.resetAllMocks();
});

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

test('editing a note fills the form with its content and its date in the display format', async () => {
  const { result } = setup();
  await act(() => result.current.handleStartEditTimelineItem('note', note));
  expect(result.current.editingTimelineItem).toEqual({ type: 'note', id: 1 });
  expect(result.current.editTimelineValues).toEqual({ noteContent: 'Hello', noteDate: '15.03.2026', itemId: 1, image: undefined });
  expect(contacts.getContacts).not.toHaveBeenCalled();
});

test('editing a note without a date leaves the date field empty', async () => {
  const { result } = setup();
  await act(() => result.current.handleStartEditTimelineItem('note', { ...note, date: '' }));
  expect(result.current.editTimelineValues.noteDate).toBe('');
});

test('editing an activity fills the form; the participant picker searches on its own', async () => {
  const { result } = setup();
  await act(() => result.current.handleStartEditTimelineItem('activity', activity));
  expect(result.current.editingTimelineItem).toEqual({ type: 'activity', id: 2 });
  expect(result.current.editTimelineValues).toEqual({
    activityTitle: 'Lunch',
    activityDescription: 'Pizza',
    activityLocation: 'Berlin',
    activityDate: '01.04.2026',
    activityContacts: [person],
    itemId: 2,
    image: undefined,
  });
  // No bulk contact fetch: the backend caps a page at 100, so it could never be complete.
  expect(contacts.getContacts).not.toHaveBeenCalled();
});

test('cancel leaves edit mode and clears the form', async () => {
  const { result } = setup();
  await act(() => result.current.handleStartEditTimelineItem('note', note));
  act(() => result.current.handleCancelEditTimelineItem());
  expect(result.current.editingTimelineItem).toBeNull();
  expect(result.current.editTimelineValues).toEqual({});
});

test('a note with blank content is not saved', async () => {
  const { result } = setup();
  await act(() => result.current.handleStartEditTimelineItem('note', { ...note, content: '   ' }));
  await act(() => result.current.handleUpdateNote(1));
  expect(notes.updateNote).not.toHaveBeenCalled();
  expect(result.current.editingTimelineItem).toEqual({ type: 'note', id: 1 });
});

test('saving a note sends the edited date, refreshes and leaves edit mode', async () => {
  const { result, onRefresh } = setup();
  await act(() => result.current.handleStartEditTimelineItem('note', note));
  act(() => result.current.setEditTimelineValues({ noteContent: 'Changed', noteDate: '20.03.2026' }));
  await act(() => result.current.handleUpdateNote(1));
  expect(notes.updateNote).toHaveBeenCalledWith(1, {
    content: 'Changed',
    date: '2026-03-20T00:00:00.000Z',
    contact_id: 5,
  });
  expect(onRefresh).toHaveBeenCalledTimes(1);
  expect(result.current.editingTimelineItem).toBeNull();
});

test('a day-first date is saved as that day, not with day and month swapped', async () => {
  const { result } = setup();
  await act(() => result.current.handleStartEditTimelineItem('note', note));
  act(() => result.current.setEditTimelineValues({ noteContent: 'Hello', noteDate: '01.05.2025' }));
  await act(() => result.current.handleUpdateNote(1));
  expect(notes.updateNote).toHaveBeenCalledWith(1, expect.objectContaining({ date: '2025-05-01T00:00:00.000Z' }));

  await act(() => result.current.handleStartEditTimelineItem('activity', activity));
  act(() => result.current.setEditTimelineValues({ ...result.current.editTimelineValues, activityDate: '30.04.2025' }));
  await act(() => result.current.handleUpdateActivity(2));
  expect(activities.updateActivity).toHaveBeenCalledWith(2, expect.objectContaining({ date: '2025-04-30T00:00:00.000Z' }));
});

test('an unreadable date shows a field error instead of saving, and clears on cancel', async () => {
  const { result, showError } = setup();
  await act(() => result.current.handleStartEditTimelineItem('note', note));
  act(() => result.current.setEditTimelineValues({ noteContent: 'Hello', noteDate: '31.02.2026' }));
  await act(() => result.current.handleUpdateNote(1));
  expect(notes.updateNote).not.toHaveBeenCalled();
  expect(result.current.editTimelineDateError).toBe('Invalid date format. Check your date format setting.');
  expect(result.current.editingTimelineItem).toEqual({ type: 'note', id: 1 });
  expect(showError).not.toHaveBeenCalled();

  act(() => result.current.handleCancelEditTimelineItem());
  expect(result.current.editTimelineDateError).toBe('');

  await act(() => result.current.handleStartEditTimelineItem('activity', activity));
  act(() => result.current.setEditTimelineValues({ ...result.current.editTimelineValues, activityDate: 'soon' }));
  await act(() => result.current.handleUpdateActivity(2));
  expect(activities.updateActivity).not.toHaveBeenCalled();
  expect(result.current.editTimelineDateError).not.toBe('');
});

test('saving a note with its date cleared falls back to now', async () => {
  vi.useFakeTimers({ toFake: ['Date'] });
  vi.setSystemTime(new Date('2026-06-15T12:00:00Z'));
  const { result } = setup();
  await act(() => result.current.handleStartEditTimelineItem('note', { ...note, date: '' }));
  await act(() => result.current.handleUpdateNote(1));
  expect(notes.updateNote).toHaveBeenCalledWith(1, expect.objectContaining({ date: '2026-06-15T12:00:00.000Z' }));
});

test('a failed note save notifies and keeps the edit open', async () => {
  quietLogs();
  notes.updateNote.mockRejectedValue(new Error('boom'));
  const { result, onRefresh, showError } = setup();
  await act(() => result.current.handleStartEditTimelineItem('note', note));
  await act(() => result.current.handleUpdateNote(1));
  expect(showError).toHaveBeenCalledWith('boom');
  expect(onRefresh).not.toHaveBeenCalled();
  expect(result.current.editTimelineValues.noteContent).toBe('Hello');
});

test('an activity with a blank title is not saved', async () => {
  const { result } = setup();
  await act(() => result.current.handleStartEditTimelineItem('activity', { ...activity, title: ' ' }));
  await act(() => result.current.handleUpdateActivity(2));
  expect(activities.updateActivity).not.toHaveBeenCalled();
});

test('saving an activity reduces its contacts to ids', async () => {
  const { result, onRefresh } = setup();
  await act(() => result.current.handleStartEditTimelineItem('activity', activity));
  await act(() => result.current.handleUpdateActivity(2));
  expect(activities.updateActivity).toHaveBeenCalledWith(2, {
    title: 'Lunch',
    description: 'Pizza',
    location: 'Berlin',
    date: '2026-04-01T00:00:00.000Z',
    contact_ids: [9],
  });
  expect(onRefresh).toHaveBeenCalledTimes(1);
  expect(result.current.editingTimelineItem).toBeNull();
});

test('a failed activity save notifies and keeps the edit open', async () => {
  quietLogs();
  activities.updateActivity.mockRejectedValue(new Error('boom'));
  const { result, showError } = setup();
  await act(() => result.current.handleStartEditTimelineItem('activity', activity));
  await act(() => result.current.handleUpdateActivity(2));
  expect(showError).toHaveBeenCalledWith('boom');
  expect(result.current.editingTimelineItem).toEqual({ type: 'activity', id: 2 });
});

test('deleting a note or activity refreshes and leaves edit mode', async () => {
  const { result, onRefresh } = setup();
  await act(() => result.current.handleStartEditTimelineItem('note', note));
  await act(() => result.current.handleDeleteNote(1));
  expect(notes.deleteNote).toHaveBeenCalledWith(1);
  expect(result.current.editingTimelineItem).toBeNull();
  await act(() => result.current.handleStartEditTimelineItem('activity', activity));
  await act(() => result.current.handleDeleteActivity(2));
  expect(activities.deleteActivity).toHaveBeenCalledWith(2);
  expect(result.current.editingTimelineItem).toBeNull();
  expect(onRefresh).toHaveBeenCalledTimes(2);
});

test('a failed delete notifies and keeps the edit open', async () => {
  quietLogs();
  notes.deleteNote.mockRejectedValue(new Error('n'));
  activities.deleteActivity.mockRejectedValue(new Error('a'));
  const { result, onRefresh, showError } = setup();
  await act(() => result.current.handleStartEditTimelineItem('note', note));
  await act(() => result.current.handleDeleteNote(1));
  await act(() => result.current.handleDeleteActivity(2));
  expect(showError.mock.calls).toEqual([['n'], ['a']]);
  expect(onRefresh).not.toHaveBeenCalled();
  expect(result.current.editingTimelineItem).toEqual({ type: 'note', id: 1 });
});

test('saving an edited note applies the staged image change before refreshing', async () => {
  const { result, onRefresh } = setup();
  const itemImages = vi.mocked(itemImagesApi);
  const file = new File(['x'], 'card.jpg', { type: 'image/jpeg' });
  const order: string[] = [];
  itemImages.applyImageChange.mockImplementation(async () => { order.push('image'); });
  onRefresh.mockImplementation(async () => { order.push('refresh'); });

  await act(() => result.current.handleStartEditTimelineItem('note', { ...note, image: 'old_image.jpg' }));
  expect(result.current.editTimelineValues.image).toBe('old_image.jpg');
  act(() => result.current.setEditTimelineValues({ ...result.current.editTimelineValues, imageChange: file }));
  await act(() => result.current.handleUpdateNote(1));

  expect(itemImages.applyImageChange).toHaveBeenCalledWith('notes', 1, file);
  expect(order).toEqual(['image', 'refresh']);
});
