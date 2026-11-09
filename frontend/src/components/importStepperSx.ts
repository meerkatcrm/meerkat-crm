import type { SxProps, Theme } from '@mui/material';

/**
 * A dialog on a phone has no room for every step name, and the stepper would scroll
 * sideways. Below the sm breakpoint only the current step shows its name. The others
 * stay available to screen readers.
 */
export const importStepperSx: SxProps<Theme> = (theme) => ({
  mb: 3,
  [theme.breakpoints.down('sm')]: {
    flexWrap: 'wrap',
    gap: 1,
    '& .MuiStepConnector-root': { display: 'none' },
    '& .MuiStep-root': { px: 0 },
    '& .MuiStepLabel-iconContainer:not(.Mui-active)': { pr: 0 },
    '& .MuiStepLabel-label:not(.Mui-active)': {
      position: 'absolute',
      width: '1px',
      height: '1px',
      overflow: 'hidden',
      clip: 'rect(0 0 0 0)',
      whiteSpace: 'nowrap',
    },
  },
});
