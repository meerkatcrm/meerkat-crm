// Optional image attached to a note or an activity
import { API_BASE_URL, apiJson } from './client';

export type ImageItemType = 'notes' | 'activities';

/**
 * A staged image change from an edit form: undefined keeps the current image,
 * null removes it, a File replaces it.
 */
export type ImageChange = File | null | undefined;

// The filename changes with every upload, so it doubles as a cache buster.
export function itemImageUrl(type: ImageItemType, id: number, image: string): string {
  return `${API_BASE_URL}/${type}/${id}/image?v=${encodeURIComponent(image)}`;
}

const MAX_SIDE = 1600;

/**
 * Re-encodes the image in the browser so phone photos arrive upright (the
 * browser applies the EXIF orientation, the server does not) and small. Falls
 * back to the original file when the browser cannot decode it (HEIC outside
 * Safari); the server converts those.
 */
async function prepareImage(file: File): Promise<Blob> {
  try {
    const bitmap = await createImageBitmap(file);
    const scale = Math.min(1, MAX_SIDE / Math.max(bitmap.width, bitmap.height));
    const canvas = document.createElement('canvas');
    canvas.width = Math.round(bitmap.width * scale);
    canvas.height = Math.round(bitmap.height * scale);
    const ctx = canvas.getContext('2d')!;
    // JPEG has no alpha: transparent areas would turn black.
    ctx.fillStyle = '#fff';
    ctx.fillRect(0, 0, canvas.width, canvas.height);
    ctx.drawImage(bitmap, 0, 0, canvas.width, canvas.height);
    bitmap.close();
    const blob = await new Promise<Blob | null>((resolve) => canvas.toBlob(resolve, 'image/jpeg', 0.85));
    return blob ?? file;
  } catch {
    return file;
  }
}

export async function uploadItemImage(type: ImageItemType, id: number, file: File): Promise<void> {
  const formData = new FormData();
  formData.append('image', await prepareImage(file), 'image.jpg');
  await apiJson(`/${type}/${id}/image`, { method: 'POST', body: formData, timeout: 60000 });
}

export async function deleteItemImage(type: ImageItemType, id: number): Promise<void> {
  await apiJson(`/${type}/${id}/image`, { method: 'DELETE' });
}

/** Applies a staged image change after its note or activity has been saved. */
export async function applyImageChange(type: ImageItemType, id: number, change: ImageChange): Promise<void> {
  if (change === null) {
    await deleteItemImage(type, id);
  } else if (change) {
    await uploadItemImage(type, id, change);
  }
}
