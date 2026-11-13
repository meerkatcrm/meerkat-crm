import { describe, test, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor, act } from '@testing-library/react';
import i18n from '../i18n/config';
import ProfilePictureUploadDialog from './ProfilePictureUploadDialog';
import * as client from '../api/client';

vi.mock('../api/client', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../api/client')>();
  return { ...actual, apiFetch: vi.fn() };
});

const envelope = (status: number, code: string, message: string, details: Record<string, string>) =>
  new Response(JSON.stringify({ error: { code, message, details } }), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });

async function fetchURL(url: string) {
  render(<ProfilePictureUploadDialog open onClose={() => {}} onUpload={async () => {}} />);
  fireEvent.change(screen.getByPlaceholderText('Paste image URL...'), { target: { value: url } });
  fireEvent.click(screen.getByRole('button', { name: 'Fetch' }));
}

describe('ProfilePictureUploadDialog image URL errors', () => {
  beforeEach(async () => {
    vi.resetAllMocks();
    await i18n.changeLanguage('en');
  });

  test('a refused URL shows a readable message, not the error details', async () => {
    vi.mocked(client.apiFetch).mockResolvedValue(
      envelope(503, 'EXTERNAL_SERVICE_ERROR', 'image proxy service error: Failed to fetch image', { service: 'image proxy' }),
    );
    await fetchURL('http://127.0.0.1/secret.png');
    expect(await screen.findByText('Failed to fetch image from URL')).toBeInTheDocument();
    expect(screen.queryByText('image proxy')).not.toBeInTheDocument();
  });

  test('the per-user limit has its own message', async () => {
    vi.mocked(client.apiFetch).mockResolvedValue(
      envelope(429, 'RATE_LIMIT_EXCEEDED', 'Rate limit exceeded. Please try again later.', {}),
    );
    await fetchURL('https://example.com/a.png');
    expect(await screen.findByText(/Too many images fetched from URLs/)).toBeInTheDocument();
  });
});

describe('ProfilePictureUploadDialog remove photo', () => {
  beforeEach(async () => {
    vi.resetAllMocks();
    await i18n.changeLanguage('en');
  });

  test('a network failure shows the translated message, not the raw error', async () => {
    const onRemove = vi.fn().mockRejectedValue(new TypeError('Failed to fetch'));
    render(<ProfilePictureUploadDialog open onClose={() => {}} onUpload={async () => {}} onRemove={onRemove} />);

    fireEvent.click(screen.getByRole('button', { name: 'Remove photo' }));

    expect(await screen.findByText('Failed to remove profile picture')).toBeInTheDocument();
    expect(screen.queryByText('Failed to fetch')).not.toBeInTheDocument();
  });

  test('there is no remove button when the contact has no photo', () => {
    render(<ProfilePictureUploadDialog open onClose={() => {}} onUpload={async () => {}} />);
    expect(screen.queryByRole('button', { name: 'Remove photo' })).not.toBeInTheDocument();
  });

  test('removing the photo closes the dialog', async () => {
    const onRemove = vi.fn().mockResolvedValue(true);
    const onClose = vi.fn();
    render(<ProfilePictureUploadDialog open onClose={onClose} onUpload={async () => {}} onRemove={onRemove} />);

    fireEvent.click(screen.getByRole('button', { name: 'Remove photo' }));

    await waitFor(() => expect(onClose).toHaveBeenCalled());
    expect(onRemove).toHaveBeenCalledTimes(1);
  });

  test('backing out of the confirmation keeps the dialog open', async () => {
    const onRemove = vi.fn().mockResolvedValue(false);
    const onClose = vi.fn();
    render(<ProfilePictureUploadDialog open onClose={onClose} onUpload={async () => {}} onRemove={onRemove} />);

    fireEvent.click(screen.getByRole('button', { name: 'Remove photo' }));

    await waitFor(() => expect(onRemove).toHaveBeenCalled());
    await waitFor(() => expect(screen.getByRole('button', { name: 'Remove photo' })).toBeEnabled());
    expect(onClose).not.toHaveBeenCalled();
  });

  test('a failed removal shows the reason and keeps the dialog open', async () => {
    const onRemove = vi.fn().mockRejectedValue(
      new client.ApiError('Photo changes are disabled in demo mode', 'FORBIDDEN', 403),
    );
    const onClose = vi.fn();
    render(<ProfilePictureUploadDialog open onClose={onClose} onUpload={async () => {}} onRemove={onRemove} />);

    fireEvent.click(screen.getByRole('button', { name: 'Remove photo' }));

    expect(await screen.findByText('Photo changes are disabled in demo mode')).toBeInTheDocument();
    expect(onClose).not.toHaveBeenCalled();
  });
});

describe('ProfilePictureUploadDialog paste from clipboard', () => {
  beforeEach(async () => {
    vi.resetAllMocks();
    await i18n.changeLanguage('en');
  });

  const paste = (target: EventTarget, files: File[], text = '') => {
    const event = new Event('paste', { bubbles: true, cancelable: true }) as Event & { clipboardData: unknown };
    event.clipboardData = { files, getData: () => text };
    target.dispatchEvent(event);
    return event;
  };

  test('a pasted image opens the cropper', async () => {
    render(<ProfilePictureUploadDialog open onClose={() => {}} onUpload={async () => {}} />);
    expect(screen.getByText(/paste an image or screenshot/)).toBeInTheDocument();

    const event = paste(document, [new File([new Uint8Array([1, 2, 3])], 'image.png', { type: 'image/png' })]);

    expect(event.defaultPrevented).toBe(true);
    expect(await screen.findByRole('slider', { name: 'Zoom' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Save' })).toBeEnabled();
  });

  test('a pasted image that is too large is refused', async () => {
    render(<ProfilePictureUploadDialog open onClose={() => {}} onUpload={async () => {}} />);
    const big = new File([new Uint8Array(1)], 'big.png', { type: 'image/png' });
    Object.defineProperty(big, 'size', { value: 11 * 1024 * 1024 });

    paste(document, [big]);

    expect(await screen.findByText('File is too large. Maximum size is 10MB.')).toBeInTheDocument();
    expect(screen.queryByRole('slider', { name: 'Zoom' })).not.toBeInTheDocument();
  });

  test('pasting text is left to the URL field', () => {
    render(<ProfilePictureUploadDialog open onClose={() => {}} onUpload={async () => {}} />);
    const field = screen.getByPlaceholderText('Paste image URL...');

    const event = paste(field, [], 'https://example.com/a.png');

    expect(event.defaultPrevented).toBe(false);
    expect(screen.queryByRole('slider', { name: 'Zoom' })).not.toBeInTheDocument();
  });

  test('a paste while a URL fetch is running is ignored', async () => {
    let finish: (r: Response) => void = () => {};
    vi.mocked(client.apiFetch).mockReturnValue(new Promise((resolve) => { finish = resolve; }));
    render(<ProfilePictureUploadDialog open onClose={() => {}} onUpload={async () => {}} />);
    fireEvent.change(screen.getByPlaceholderText('Paste image URL...'), { target: { value: 'https://example.com/a.png' } });
    fireEvent.click(screen.getByRole('button', { name: 'Fetch' }));

    const event = paste(document, [new File([new Uint8Array([1])], 'image.png', { type: 'image/png' })]);

    expect(event.defaultPrevented).toBe(false);
    finish(new Response(null, { status: 503 }));
    expect(await screen.findByText('Failed to fetch image from URL')).toBeInTheDocument();
  });

  test('the image started last wins, whichever read finishes first', async () => {
    const readers: { result: string; listener: () => void; file: File }[] = [];
    class ControlledReader {
      result = '';
      private entry?: { result: string; listener: () => void; file: File };
      addEventListener(_type: string, listener: () => void) {
        this.entry = { result: '', listener, file: new File([], '') };
      }
      readAsDataURL(file: File) {
        const entry = this.entry!;
        entry.file = file;
        entry.listener = entry.listener.bind(this);
        readers.push(entry);
        this.result = `data:${file.type};name=${file.name}`;
      }
    }
    vi.stubGlobal('FileReader', ControlledReader);
    try {
      render(<ProfilePictureUploadDialog open onClose={() => {}} onUpload={async () => {}} />);
      paste(document, [new File([new Uint8Array([1])], 'first.png', { type: 'image/png' })]);
      paste(document, [new File([new Uint8Array([2])], 'second.png', { type: 'image/png' })]);
      expect(readers).toHaveLength(2);

      act(() => readers[1].listener());
      act(() => readers[0].listener());

      await screen.findByRole('slider', { name: 'Zoom' });
      expect(document.querySelector('img')?.getAttribute('src')).toContain('second.png');
    } finally {
      vi.unstubAllGlobals();
    }
  });

  test('a URL fetch that finishes after the dialog closed does not show up when it reopens', async () => {
    let finish: (r: Response) => void = () => {};
    let signal: AbortSignal | undefined;
    vi.mocked(client.apiFetch).mockImplementation((_url, options) => {
      signal = options?.signal ?? undefined;
      return new Promise((resolve) => { finish = resolve; });
    });
    const { rerender } = render(<ProfilePictureUploadDialog open onClose={() => {}} onUpload={async () => {}} />);
    fireEvent.change(screen.getByPlaceholderText('Paste image URL...'), { target: { value: 'https://example.com/a.png' } });
    fireEvent.click(screen.getByRole('button', { name: 'Fetch' }));

    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(signal?.aborted).toBe(true);
    finish(new Response(new Uint8Array([1, 2, 3]), { status: 200, headers: { 'Content-Type': 'image/png' } }));
    rerender(<ProfilePictureUploadDialog open={false} onClose={() => {}} onUpload={async () => {}} />);
    rerender(<ProfilePictureUploadDialog open onClose={() => {}} onUpload={async () => {}} />);

    await waitFor(() => expect(screen.getByRole('button', { name: 'Fetch' })).toBeInTheDocument());
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(screen.queryByRole('slider', { name: 'Zoom' })).not.toBeInTheDocument();
    expect(screen.getByPlaceholderText('Paste image URL...')).toHaveValue('');
  });

  test('a closed dialog ignores pastes', () => {
    render(<ProfilePictureUploadDialog open={false} onClose={() => {}} onUpload={async () => {}} />);
    const event = paste(document, [new File([new Uint8Array([1])], 'image.png', { type: 'image/png' })]);
    expect(event.defaultPrevented).toBe(false);
  });
});
