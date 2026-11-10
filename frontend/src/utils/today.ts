import { format } from 'date-fns';

// Today's calendar date where the user is, as YYYY-MM-DD. toISOString() gives
// the UTC day instead, which is yesterday or tomorrow near local midnight.
export function todayISO(): string {
  return format(new Date(), 'yyyy-MM-dd');
}
