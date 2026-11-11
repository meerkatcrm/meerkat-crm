import { test, expect, afterEach } from 'vitest';
import { render, screen, cleanup } from '@testing-library/react';
import { I18nextProvider } from 'react-i18next';
import i18n from '../i18n/config';
import NetworkLegend from './NetworkLegend';

afterEach(cleanup);

const renderLegend = (props: React.ComponentProps<typeof NetworkLegend>) =>
  render(
    <I18nextProvider i18n={i18n}>
      <NetworkLegend {...props} />
    </I18nextProvider>
  );

test('shows a dashed "Ended relationship" entry when ended relationships are drawn', () => {
  renderLegend({ showRelationships: true, showEndedRelationships: true });
  expect(screen.getByText('Ended relationship')).toBeInTheDocument();
  expect(screen.getByTestId('legend-dashed-line')).toBeInTheDocument();
});

test.each([
  [{ showRelationships: true, showEndedRelationships: false }],
  [{ showRelationships: false, showEndedRelationships: true }],
])('hides the ended entry when it is not drawn (%o)', (props) => {
  renderLegend(props);
  expect(screen.queryByText('Ended relationship')).not.toBeInTheDocument();
});
