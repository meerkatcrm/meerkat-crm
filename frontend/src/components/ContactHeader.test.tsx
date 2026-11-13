import { test, expect, vi, afterEach, beforeEach } from 'vitest';
import { render, screen, fireEvent, cleanup } from '@testing-library/react';
import { I18nextProvider } from 'react-i18next';
import i18n from '../i18n/config';
import ContactHeader, { ProfileValues } from './ContactHeader';

const profileValues: ProfileValues = { prefix: '', firstname: 'Ada', middle_name: '', lastname: 'Lovelace', suffix: '', nickname: '', gender: '' };

beforeEach(() => {
  i18n.changeLanguage('en');
});

afterEach(() => {
  cleanup();
});

function renderHeader(props: { isSelf?: boolean; onToggleSelf?: () => void }) {
  const noop = () => {};
  render(
    <I18nextProvider i18n={i18n}>
      <ContactHeader
        contact={{ ID: 1, firstname: 'Ada', lastname: 'Lovelace' }}
        profilePic=""
        editingProfile={false}
        profileValues={profileValues}
        editingCircles={false}
        newCircleName=""
        availableCircles={[]}
        onStartEditProfile={noop}
        onCancelEditProfile={noop}
        onSaveProfile={noop}
        onDeleteContact={noop}
        onProfileValueChange={noop}
        onToggleEditCircles={noop}
        onAddCircle={noop}
        onDeleteCircle={noop}
        onNewCircleNameChange={noop}
        {...props}
      />
    </I18nextProvider>
  );
}

test('a contact that is not "me" offers to mark it and shows no badge', () => {
  const onToggleSelf = vi.fn();
  renderHeader({ isSelf: false, onToggleSelf });

  expect(screen.queryByText('Me')).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: 'This is me' }));
  expect(onToggleSelf).toHaveBeenCalledTimes(1);
});

test('the "me" contact shows a badge and offers to clear the mark', () => {
  const onToggleSelf = vi.fn();
  renderHeader({ isSelf: true, onToggleSelf });

  expect(screen.getByText('Me')).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'This is not me' }));
  expect(onToggleSelf).toHaveBeenCalledTimes(1);
});

test('without a handler the toggle is hidden', () => {
  renderHeader({});
  expect(screen.queryByRole('button', { name: 'This is me' })).toBeNull();
});
