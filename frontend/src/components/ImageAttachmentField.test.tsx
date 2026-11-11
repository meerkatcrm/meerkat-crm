import { test, expect, vi, afterEach, beforeAll } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { I18nextProvider } from 'react-i18next';
import i18n from '../i18n/config';
import ImageAttachmentField from './ImageAttachmentField';
import { ImageChange } from '../api/itemImages';

beforeAll(() => {
  // jsdom has no object URLs.
  URL.createObjectURL = vi.fn(() => 'blob:preview');
  URL.revokeObjectURL = vi.fn();
});
afterEach(cleanup);

function renderField(props: { id?: number; image?: string; value: ImageChange; onChange?: (v: ImageChange) => void }) {
  const onChange = props.onChange ?? vi.fn();
  render(
    <I18nextProvider i18n={i18n}>
      <ImageAttachmentField type="notes" {...props} onChange={onChange} />
    </I18nextProvider>
  );
  return onChange;
}

const file = new File(['x'], 'card.jpg', { type: 'image/jpeg' });

test('without an image only offers to attach one, and picking a file stages it', () => {
  const onChange = renderField({ value: undefined });
  expect(screen.queryByRole('button', { name: 'Remove image' })).toBeNull();
  expect(screen.queryByRole('img')).toBeNull();

  fireEvent.change(screen.getByText('Attach image').querySelector('input')!, { target: { files: [file] } });
  expect(onChange).toHaveBeenCalledWith(file);
});

test('a staged file is previewed before it is uploaded', () => {
  renderField({ value: file });
  expect(screen.getByRole('img')).toHaveAttribute('src', 'blob:preview');
  expect(screen.getByText('Replace image')).toBeInTheDocument();
});

test('the saved image is shown and removing it stages a delete', () => {
  const onChange = renderField({ id: 4, image: 'abc_image.jpg', value: undefined });
  expect(screen.getByRole('img').getAttribute('src')).toContain('/notes/4/image?v=abc_image.jpg');

  fireEvent.click(screen.getByRole('button', { name: 'Remove image' }));
  expect(onChange).toHaveBeenCalledWith(null);
});

test('once removed, the saved image is no longer shown', () => {
  renderField({ id: 4, image: 'abc_image.jpg', value: null });
  expect(screen.queryByRole('img')).toBeNull();
  expect(screen.getByText('Attach image')).toBeInTheDocument();
});

test('removing a newly picked file when nothing was saved just drops it', () => {
  const onChange = renderField({ value: file });
  fireEvent.click(screen.getByRole('button', { name: 'Remove image' }));
  expect(onChange).toHaveBeenCalledWith(undefined);
});
