import { test, expect, vi, afterEach } from 'vitest';
import { render, screen, cleanup } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { I18nextProvider } from 'react-i18next';
import i18n from '../i18n/config';
import { DateFormatProvider } from '../DateFormatProvider';
import RelationshipList from './RelationshipList';
import { Relationship, IncomingRelationship } from '../api/relationships';

afterEach(() => {
  cleanup();
  window.localStorage.removeItem('dateFormat');
});

const rel = (over: Partial<Relationship> = {}): Relationship => ({
  ID: 1,
  CreatedAt: '',
  UpdatedAt: '',
  name: 'Sam',
  type: 'Spouse',
  contact_id: 1,
  ...over,
});

function renderList(relationships: Relationship[], incomingRelationships: IncomingRelationship[] = []) {
  return render(
    <MemoryRouter>
      <I18nextProvider i18n={i18n}>
        <DateFormatProvider>
          <RelationshipList
            relationships={relationships}
            incomingRelationships={incomingRelationships}
            onEdit={vi.fn()}
            onDelete={vi.fn()}
          />
        </DateFormatProvider>
      </I18nextProvider>
    </MemoryRouter>
  );
}

test('ongoing relationship shows "since" and no Ended chip', () => {
  window.localStorage.setItem('dateFormat', 'eu');
  const { container } = renderList([rel({ start_date: '2010-03-15' })]);
  expect(screen.getByText('since 15.03.2010')).toBeInTheDocument();
  expect(screen.queryByText('Ended')).not.toBeInTheDocument();
  expect(container.querySelector('[data-ended]')).toBeNull();
});

test.each([
  ['eu', '06.2018'],
  ['us', '06/2018'],
  ['iso', '2018-06'],
])('%s: ended relationship is marked and its date uses the preferred format', (format, end) => {
  window.localStorage.setItem('dateFormat', format);
  const { container } = renderList([rel({ end_date: '2018-06' })]);
  expect(screen.getByText('Ended')).toBeInTheDocument();
  expect(screen.getByText(`ended ${end}`)).toBeInTheDocument();
  expect(container.querySelector('[data-ended]')).not.toBeNull();
});

test('relationship with both dates shows a range', () => {
  window.localStorage.setItem('dateFormat', 'iso');
  renderList([rel({ start_date: '2010', end_date: '2018-06-30' })]);
  expect(screen.getByText('2010 – 2018-06-30')).toBeInTheDocument();
});

test('relationship without dates shows no date line', () => {
  renderList([rel()]);
  expect(screen.queryByText(/since|ended/)).not.toBeInTheDocument();
});

test('ended incoming relationship is marked too', () => {
  renderList([], [{
    ID: 2, CreatedAt: '', UpdatedAt: '', name: 'Sam', type: 'Spouse', contact_id: 3, end_date: '2019',
    source_contact: { ID: 3, firstname: 'Alex', lastname: 'Roe' },
  }]);
  expect(screen.getByText('Ended')).toBeInTheDocument();
  expect(screen.getByText('ended 2019')).toBeInTheDocument();
});

test('a start date still ahead reads "starting", not "since"', () => {
  window.localStorage.setItem('dateFormat', 'iso');
  const nextYear = String(new Date().getFullYear() + 1);
  renderList([rel({ type: 'Partner', start_date: `${nextYear}-06` })]);
  expect(screen.getByText(`starting ${nextYear}-06`)).toBeInTheDocument();
  expect(screen.queryByText(/since/)).not.toBeInTheDocument();
});

test('a start in the current year still reads "since"', () => {
  const thisYear = String(new Date().getFullYear());
  renderList([rel({ start_date: thisYear })]);
  expect(screen.getByText(`since ${thisYear}`)).toBeInTheDocument();
});
