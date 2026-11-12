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
  // A plain <img> cannot tell a 401 (expired session) from a missing file;
  // either way, show nothing rather than a broken-image icon.
  const [failedSrc, setFailedSrc] = useState<string>();
  if (!image) return null;
  const src = itemImageUrl(type, id, image);
  if (failedSrc === src) return null;

  return (
    <>
      <Box
        component="img"
        src={src}
        alt={t('itemImage.alt')}
        loading="lazy"
        onError={() => setFailedSrc(src)}
        role="button"
        tabIndex={0}
        onClick={() => setOpen(true)}
        onKeyDown={(e: React.KeyboardEvent) => {
          if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault();
            setOpen(true);
          }
        }}
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
