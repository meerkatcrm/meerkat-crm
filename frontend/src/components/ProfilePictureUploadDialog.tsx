import { useState, useCallback, useEffect, useRef } from 'react';
import { useTranslation } from 'react-i18next';
import Cropper, { Area } from 'react-easy-crop';
import {
  DialogTitle,
  DialogContent,
  DialogActions,
  Button,
  Box,
  Slider,
  Typography,
  CircularProgress,
  Alert,
  TextField,
  Divider
} from '@mui/material';
import AppDialog from './AppDialog';
import ZoomInIcon from '@mui/icons-material/ZoomIn';
import CloudUploadIcon from '@mui/icons-material/CloudUpload';
import LinkIcon from '@mui/icons-material/Link';
import DeleteIcon from '@mui/icons-material/Delete';
import { handleError, getErrorMessage } from '../utils/errorHandler';
import { API_BASE_URL, ApiError, apiFetch, parseErrorResponse } from '../api/client';

interface ProfilePictureUploadDialogProps {
  open: boolean;
  onClose: () => void;
  onUpload: (croppedImageBlob: Blob) => Promise<void>;
  /**
   * Shown as a "Remove photo" button when the contact has a photo. Resolves to true when
   * the photo was removed (the dialog then closes), false when the user backed out.
   */
  onRemove?: () => Promise<boolean>;
}

const MAX_FILE_SIZE = 10 * 1024 * 1024; // 10MB

// Helper function to create a cropped image blob
async function getCroppedImg(
  imageSrc: string,
  pixelCrop: Area,
  outputSize: number = 400
): Promise<Blob> {
  const image = await createImage(imageSrc);
  const canvas = document.createElement('canvas');
  const ctx = canvas.getContext('2d');

  if (!ctx) {
    throw new Error('Could not get canvas context');
  }

  // Set canvas size to desired output size
  canvas.width = outputSize;
  canvas.height = outputSize;

  // Draw the cropped image
  ctx.drawImage(
    image,
    pixelCrop.x,
    pixelCrop.y,
    pixelCrop.width,
    pixelCrop.height,
    0,
    0,
    outputSize,
    outputSize
  );

  // Return as blob
  return new Promise((resolve, reject) => {
    canvas.toBlob(
      (blob) => {
        if (blob) {
          resolve(blob);
        } else {
          reject(new Error('Canvas is empty'));
        }
      },
      'image/jpeg',
      0.9
    );
  });
}

// Helper function to load an image
function createImage(url: string): Promise<HTMLImageElement> {
  return new Promise((resolve, reject) => {
    const image = new Image();
    image.addEventListener('load', () => resolve(image));
    image.addEventListener('error', (error) => reject(error));
    image.src = url;
  });
}

export default function ProfilePictureUploadDialog({
  open,
  onClose,
  onUpload,
  onRemove
}: ProfilePictureUploadDialogProps) {
  const { t } = useTranslation();
  const [imageSrc, setImageSrc] = useState<string | null>(null);
  const [crop, setCrop] = useState({ x: 0, y: 0 });
  const [zoom, setZoom] = useState(1);
  const [croppedAreaPixels, setCroppedAreaPixels] = useState<Area | null>(null);
  const [uploading, setUploading] = useState(false);
  const [removing, setRemoving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [imageUrl, setImageUrl] = useState('');
  const [fetchingUrl, setFetchingUrl] = useState(false);
  const busy = uploading || removing;

  const onCropComplete = useCallback((_croppedArea: Area, croppedAreaPixels: Area) => {
    setCroppedAreaPixels(croppedAreaPixels);
  }, []);

  // Picker, drop, paste and URL fetch all load through loadFile; FileReader is async, so
  // only the most recently started load may set the image, whatever order they finish in.
  const loadGeneration = useRef(0);

  const loadFile = useCallback((file: File) => {
    const generation = ++loadGeneration.current;
    setError(null);

    // Validate file type
    if (!file.type.startsWith('image/')) {
      setError(t('profilePicture.invalidFileType'));
      return;
    }

    // Validate file size
    if (file.size > MAX_FILE_SIZE) {
      setError(t('profilePicture.fileTooLarge'));
      return;
    }

    const reader = new FileReader();
    reader.addEventListener('load', () => {
      if (generation !== loadGeneration.current) return;
      setImageSrc(reader.result as string);
      setCrop({ x: 0, y: 0 });
      setZoom(1);
    });
    reader.readAsDataURL(file);
  }, [t]);

  const handleFileSelect = (event: React.ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0];
    // Cleared so that picking the same file again still fires a change event.
    event.target.value = '';
    if (file) loadFile(file);
  };

  // Without preventDefault the browser opens a dropped file, navigating away from the app.
  const handleDragOver = (event: React.DragEvent<HTMLElement>) => {
    event.preventDefault();
  };

  const handleDrop = (event: React.DragEvent<HTMLElement>) => {
    event.preventDefault();
    const file = event.dataTransfer.files?.[0];
    if (file) loadFile(file);
  };

  // Ctrl+V / Cmd+V anywhere in the open dialog: a pasted image (a screenshot, an image
  // copied in a browser) goes through the same checks and cropper as a picked file.
  // Pastes without an image are left alone, so text still pastes into the URL field.
  // A URL fetch in flight would replace the pasted image when it finishes.
  const pasteBlocked = uploading || removing || fetchingUrl;
  useEffect(() => {
    if (!open || pasteBlocked) return;
    const handlePaste = (event: ClipboardEvent) => {
      const image = Array.from(event.clipboardData?.files ?? []).find((file) => file.type.startsWith('image/'));
      if (!image) return;
      event.preventDefault();
      loadFile(image);
    };
    document.addEventListener('paste', handlePaste);
    return () => document.removeEventListener('paste', handlePaste);
  }, [open, pasteBlocked, loadFile]);

  // Aborted by handleClose, so a fetch still running when the dialog closes cannot load
  // its image (or an error) into the dialog for the next time it opens.
  const fetchAbort = useRef<AbortController | null>(null);

  const handleFetchFromUrl = async () => {
    if (!imageUrl.trim()) return;

    fetchAbort.current?.abort();
    const controller = new AbortController();
    fetchAbort.current = controller;
    setError(null);
    setFetchingUrl(true);

    try {
      const proxyUrl = `${API_BASE_URL}/proxy/image?url=${encodeURIComponent(imageUrl.trim())}`;

      const response = await apiFetch(proxyUrl, { signal: controller.signal });

      if (!response.ok) {
        // The server does not say why a fetch failed (that would tell which internal hosts exist),
        // so only the rate limit gets its own message.
        const apiError = await parseErrorResponse(response);
        throw new Error(
          apiError.code === 'RATE_LIMIT_EXCEEDED' ? t('profilePicture.urlRateLimited') : t('profilePicture.urlFetchError'),
        );
      }

      const contentType = response.headers.get('content-type');
      if (!contentType || !contentType.startsWith('image/')) {
        throw new Error(t('profilePicture.invalidFileType'));
      }

      const blob = await response.blob();
      if (controller.signal.aborted) return;

      if (blob.size > MAX_FILE_SIZE) {
        throw new Error(t('profilePicture.fileTooLarge'));
      }

      loadFile(new File([blob], 'image', { type: blob.type }));
      setImageUrl('');
    } catch (err) {
      if (controller.signal.aborted) return;
      handleError(err, { operation: 'fetching image from URL' });
      setError(getErrorMessage(err));
    } finally {
      if (fetchAbort.current === controller) {
        fetchAbort.current = null;
        setFetchingUrl(false);
      }
    }
  };

  const handleUpload = async () => {
    if (!imageSrc || !croppedAreaPixels) return;

    setUploading(true);
    setError(null);

    try {
      const croppedImage = await getCroppedImg(imageSrc, croppedAreaPixels);
      await onUpload(croppedImage);
      handleClose();
    } catch (err) {
      handleError(err, { operation: 'uploading profile picture' });
      setError(getErrorMessage(err));
    } finally {
      setUploading(false);
    }
  };

  const handleRemove = async () => {
    if (!onRemove) return;

    setRemoving(true);
    setError(null);

    try {
      if (await onRemove()) {
        handleClose();
      }
    } catch (err) {
      handleError(err, { operation: 'removing profile picture' });
      // The server's reason (e.g. demo mode) when there is one; network errors and
      // timeouts arrive as untranslated plain Errors, so those get the locale string.
      setError(err instanceof ApiError ? err.getDisplayMessage() : t('profilePicture.removeError'));
    } finally {
      setRemoving(false);
    }
  };

  const handleClose = () => {
    // A fetch or read still in flight must not put its image into the dialog when it is reopened.
    fetchAbort.current?.abort();
    fetchAbort.current = null;
    loadGeneration.current++;
    setImageSrc(null);
    setCrop({ x: 0, y: 0 });
    setZoom(1);
    setCroppedAreaPixels(null);
    setError(null);
    setImageUrl('');
    setFetchingUrl(false);
    onClose();
  };

  return (
    <AppDialog open={open} onClose={handleClose} busy={busy} maxWidth="sm" fullWidth>
      <DialogTitle>{t('profilePicture.title')}</DialogTitle>
      <DialogContent>
        {error && (
          <Alert severity="error" sx={{ mb: 2 }}>
            {error}
          </Alert>
        )}

        {!imageSrc ? (
          // File selection view
          <Box>
            <Box
              sx={{
                display: 'flex',
                flexDirection: 'column',
                alignItems: 'center',
                justifyContent: 'center',
                minHeight: 200,
                border: '2px dashed',
                borderColor: 'divider',
                borderRadius: 2,
                p: 4,
                cursor: 'pointer',
                '&:hover': {
                  borderColor: 'primary.main',
                  bgcolor: 'action.hover'
                }
              }}
              component="label"
              onDragOver={handleDragOver}
              onDrop={handleDrop}
            >
              <input
                type="file"
                accept="image/*"
                onChange={handleFileSelect}
                style={{ display: 'none' }}
              />
              <CloudUploadIcon sx={{ fontSize: 48, color: 'text.secondary', mb: 1 }} />
              <Typography variant="h6" sx={{
                color: "text.secondary"
              }}>
                {t('profilePicture.selectImage')}
              </Typography>
              <Typography variant="body2" sx={{
                color: "text.secondary"
              }}>
                {t('profilePicture.dragOrClick')}
              </Typography>
              <Typography variant="body2" sx={{
                color: "text.secondary",
                mt: 0.5
              }}>
                {t('profilePicture.pasteHint')}
              </Typography>
            </Box>

            <Divider sx={{ my: 3 }}>
              <Typography variant="body2" sx={{
                color: "text.secondary"
              }}>
                {t('profilePicture.or')}
              </Typography>
            </Divider>

            <Box sx={{ display: 'flex', gap: 1, alignItems: 'flex-start' }}>
              <TextField
                fullWidth
                size="small"
                placeholder={t('profilePicture.urlPlaceholder')}
                value={imageUrl}
                onChange={(e) => setImageUrl(e.target.value)}
                disabled={fetchingUrl}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') {
                    e.preventDefault();
                    handleFetchFromUrl();
                  }
                }}
                slotProps={{
                  input: {
                    startAdornment: <LinkIcon sx={{ mr: 1, color: 'text.secondary' }} />
                  }
                }}
              />
              <Button
                variant="outlined"
                onClick={handleFetchFromUrl}
                disabled={!imageUrl.trim() || fetchingUrl}
                sx={{ minWidth: 100 }}
              >
                {fetchingUrl ? <CircularProgress size={20} /> : t('profilePicture.fetch')}
              </Button>
            </Box>
          </Box>
        ) : (
          // Cropping view
          <Box>
            <Box
              sx={{
                position: 'relative',
                width: '100%',
                height: 350,
                bgcolor: 'black',
                borderRadius: 1,
                overflow: 'hidden'
              }}
            >
              <Cropper
                image={imageSrc}
                crop={crop}
                zoom={zoom}
                aspect={1}
                onCropChange={setCrop}
                onZoomChange={setZoom}
                onCropComplete={onCropComplete}
                cropShape="round"
                showGrid={false}
              />
            </Box>

            {/* Zoom slider */}
            <Box sx={{ display: 'flex', alignItems: 'center', mt: 2, px: 2 }}>
              <ZoomInIcon sx={{ mr: 2, color: 'text.secondary' }} />
              <Slider
                value={zoom}
                min={1}
                max={3}
                step={0.1}
                onChange={(_, value) => setZoom(value as number)}
                aria-label={t('profilePicture.zoom')}
              />
            </Box>

            {/* Change image button */}
            <Box sx={{ display: 'flex', justifyContent: 'center', mt: 1 }}>
              <Button
                component="label"
                size="small"
                variant="text"
              >
                {t('profilePicture.changeImage')}
                <input
                  type="file"
                  accept="image/*"
                  onChange={handleFileSelect}
                  style={{ display: 'none' }}
                />
              </Button>
            </Box>
          </Box>
        )}
      </DialogContent>
      <DialogActions>
        {onRemove && !imageSrc && (
          <Button
            onClick={handleRemove}
            color="error"
            disabled={busy}
            startIcon={removing ? <CircularProgress size={20} /> : <DeleteIcon />}
            sx={{ mr: 'auto' }}
          >
            {t('profilePicture.remove')}
          </Button>
        )}
        <Button onClick={handleClose} disabled={busy}>
          {t('common.cancel')}
        </Button>
        <Button
          onClick={handleUpload}
          variant="contained"
          disabled={!imageSrc || busy}
          startIcon={uploading ? <CircularProgress size={20} /> : undefined}
        >
          {uploading ? t('profilePicture.uploading') : t('common.save')}
        </Button>
      </DialogActions>
    </AppDialog>
  );
}
