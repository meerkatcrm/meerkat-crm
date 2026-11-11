import { useState } from 'react';
import { Box, Dialog } from '@mui/material';
import { useTranslation } from 'react-i18next';
import { ImageItemType, itemImageUrl } from '../api/itemImages';

interface ItemImageProps {
  type: ImageItemType;
  id: number;
  image?: string;
  maxHeight?: number;
}

/** The image attached to a note or activity; a click shows it full size. */
export default function ItemImage({ type, id, image, maxHeight = 200 }: ItemImageProps) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  if (!image) return null;
  const src = itemImageUrl(type, id, image);

  return (
    <>
      <Box
        component="img"
        src={src}
        alt={t('itemImage.alt')}
        loading="lazy"
        onClick={() => setOpen(true)}
        sx={{ display: 'block', mt: 1, maxWidth: '100%', maxHeight, borderRadius: 1, cursor: 'zoom-in' }}
      />
      <Dialog open={open} onClose={() => setOpen(false)} maxWidth="lg">
        <Box
          component="img"
          src={src}
          alt={t('itemImage.alt')}
          onClick={() => setOpen(false)}
          sx={{ display: 'block', maxWidth: '100%', maxHeight: '90vh', cursor: 'zoom-out' }}
        />
      </Dialog>
    </>
  );
}
