import { test, expect, afterEach, beforeEach } from 'vitest';
import { render, screen, cleanup } from '@testing-library/react';
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

function renderHeader(props: { isSelf?: boolean }) {
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

test('the "me" contact shows a badge', () => {
  renderHeader({ isSelf: true });
  expect(screen.getByText('Me')).toBeInTheDocument();
});

test('another contact shows no badge and no way to mark it here (that lives in Settings)', () => {
  renderHeader({ isSelf: false });
  expect(screen.queryByText('Me')).toBeNull();
  expect(screen.queryByRole('button', { name: /me/i })).toBeNull();
});
