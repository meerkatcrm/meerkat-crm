import { format } from 'date-fns';
import { now } from './now';

// Today's calendar date where the user is, as YYYY-MM-DD. toISOString() gives
// the UTC day instead, which is yesterday or tomorrow near local midnight.
export function todayISO(): string {
  return format(now(), 'yyyy-MM-dd');
}
