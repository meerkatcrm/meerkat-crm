import type { SxProps, Theme } from '@mui/material';

const NARROW_CONTAINER = '@container (max-width: 480px)';

/**
 * Keeps a date | dot | card timeline readable when it has little room: the date
 * moves above the card so the card gets the full remaining width. Uses a container
 * query because the contact page's timeline is narrow in its two-column layout too,
 * at viewport widths where a breakpoint wouldn't apply.
 */
export const responsiveTimelineSx: SxProps<Theme> = {
  containerType: 'inline-size',
  '& .MuiTimelineItem-root': {
    [NARROW_CONTAINER]: {
      display: 'grid',
      gridTemplateColumns: 'auto minmax(0, 1fr)',
      gridTemplateRows: 'auto 1fr',
    },
  },
  '& .MuiTimelineOppositeContent-root': {
    [NARROW_CONTAINER]: {
      gridColumn: 2,
      gridRow: 1,
      textAlign: 'left',
      pb: 0,
    },
  },
  '& .MuiTimelineSeparator-root': {
    [NARROW_CONTAINER]: {
      gridColumn: 1,
      gridRow: '1 / span 2',
    },
  },
  '& .MuiTimelineContent-root': {
    [NARROW_CONTAINER]: {
      gridColumn: 2,
      gridRow: 2,
      pr: 0,
    },
  },
};
