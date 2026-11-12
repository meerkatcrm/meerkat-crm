import { useEffect, useState } from 'react';
import { Box, Button, Typography } from '@mui/material';
import AddPhotoAlternateIcon from '@mui/icons-material/AddPhotoAlternate';
import DeleteIcon from '@mui/icons-material/Delete';
import ImageIcon from '@mui/icons-material/Image';
import { useTranslation } from 'react-i18next';
import { ImageChange, ImageItemType } from '../api/itemImages';
import { isDemoMode } from '../auth';
import ItemImage from './ItemImage';

interface ImageAttachmentFieldProps {
  type: ImageItemType;
  /** The saved note or activity, when editing one. */
  id?: number;
  /** Its current image filename. */
  image?: string;
  value: ImageChange;
  onChange: (value: ImageChange) => void;
}

/** Picks, previews or removes the one image of a note or activity. Nothing is uploaded until the form saves. */
export default function ImageAttachmentField({ type, id, image, value, onChange }: ImageAttachmentFieldProps) {
  const { t } = useTranslation();
  const [previewUrl, setPreviewUrl] = useState<string>();
  // Some formats the server accepts cannot be shown here (HEIC outside Safari).
  const [previewFailed, setPreviewFailed] = useState(false);

  useEffect(() => {
    setPreviewFailed(false);
    if (!value) {
      setPreviewUrl(undefined);
      return;
    }
    const url = URL.createObjectURL(value);
    setPreviewUrl(url);
    return () => URL.revokeObjectURL(url);
  }, [value]);

  const hasImage = value ? true : value === undefined && !!image;
  // The server refuses image changes in demo mode: show the saved image, offer no controls.
  if (isDemoMode()) {
    return hasImage && id !== undefined ? <ItemImage type={type} id={id} image={image} /> : null;
  }

  return (
    <Box>
      {value && previewFailed ? (
        <Typography variant="body2" sx={{ display: 'flex', alignItems: 'center', gap: 1, color: 'text.secondary' }}>
          <ImageIcon fontSize="small" />
          {t('itemImage.noPreview', { name: value.name })}
        </Typography>
      ) : previewUrl ? (
        <Box
          component="img"
          src={previewUrl}
          alt={t('itemImage.alt')}
          onError={() => setPreviewFailed(true)}
          sx={{ display: 'block', mb: 1, maxWidth: '100%', maxHeight: 200, borderRadius: 1 }}
        />
      ) : (
        hasImage && id !== undefined && <ItemImage type={type} id={id} image={image} />
      )}
      <Box sx={{ display: 'flex', gap: 1, mt: hasImage ? 1 : 0 }}>
        <Button component="label" size="small" startIcon={<AddPhotoAlternateIcon />}>
          {hasImage ? t('itemImage.replace') : t('itemImage.attach')}
          <input
            hidden
            type="file"
            accept="image/*"
            onChange={(e) => {
              const file = e.target.files?.[0];
              if (file) onChange(file);
              e.target.value = '';
            }}
          />
        </Button>
        {/* Remove first drops a newly picked file (back to the saved image, if any);
            only a shown saved image is staged for deletion. */}
        {hasImage && (
          <Button size="small" color="error" startIcon={<DeleteIcon />} onClick={() => onChange(value ? undefined : null)}>
            {t('itemImage.remove')}
          </Button>
        )}
      </Box>
    </Box>
  );
}
