import { useEffect, useState } from 'react';
import { Box, Button } from '@mui/material';
import AddPhotoAlternateIcon from '@mui/icons-material/AddPhotoAlternate';
import DeleteIcon from '@mui/icons-material/Delete';
import { useTranslation } from 'react-i18next';
import { ImageChange, ImageItemType } from '../api/itemImages';
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

  useEffect(() => {
    if (!value) {
      setPreviewUrl(undefined);
      return;
    }
    const url = URL.createObjectURL(value);
    setPreviewUrl(url);
    return () => URL.revokeObjectURL(url);
  }, [value]);

  const hasImage = value ? true : value === undefined && !!image;

  return (
    <Box>
      {previewUrl ? (
        <Box
          component="img"
          src={previewUrl}
          alt={t('itemImage.alt')}
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
        {hasImage && (
          <Button size="small" color="error" startIcon={<DeleteIcon />} onClick={() => onChange(image ? null : undefined)}>
            {t('itemImage.remove')}
          </Button>
        )}
      </Box>
    </Box>
  );
}
