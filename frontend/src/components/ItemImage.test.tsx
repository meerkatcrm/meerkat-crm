import { test, expect, afterEach } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { I18nextProvider } from 'react-i18next';
import i18n from '../i18n/config';
import ItemImage from './ItemImage';

afterEach(cleanup);

function renderImage(image?: string) {
  return render(
    <I18nextProvider i18n={i18n}>
      <ItemImage type="activities" id={3} image={image} />
    </I18nextProvider>
  );
}

test('renders nothing without an image', () => {
  renderImage(undefined);
  expect(screen.queryByRole('button', { name: 'Attached image' })).toBeNull();
});

test('an image that fails to load is hidden instead of shown broken', () => {
  renderImage('a_image.jpg');
  const img = screen.getByRole('button', { name: 'Attached image' });
  expect(img.getAttribute('src')).toContain('/activities/3/image?v=a_image.jpg');

  fireEvent.error(img);

  expect(screen.queryByRole('button', { name: 'Attached image' })).toBeNull();
});

test('a new image after a failed one is shown again', () => {
  const { rerender } = renderImage('a_image.jpg');
  fireEvent.error(screen.getByRole('button', { name: 'Attached image' }));

  rerender(
    <I18nextProvider i18n={i18n}>
      <ItemImage type="activities" id={3} image="b_image.jpg" />
    </I18nextProvider>
  );

  expect(screen.getByRole('button', { name: 'Attached image' }).getAttribute('src')).toContain('v=b_image.jpg');
});
