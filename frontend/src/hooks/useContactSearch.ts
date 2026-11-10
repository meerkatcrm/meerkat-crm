import { useEffect, useState } from 'react';
import { Contact, getContacts } from '../api/contacts';
import { handleFetchError } from '../utils/errorHandler';
import { useDebouncedValue } from './useDebounce';

interface ContactSearchOptions {
  /** Contact to leave out of the results (e.g. the one being edited). */
  excludeId?: number;
  includeArchived?: boolean;
}

// The backend clamps a page to 100 contacts; further matches are reached by typing.
const SEARCH_LIMIT = 100;
const SEARCH_DEBOUNCE_MS = 300;

/**
 * Server-side contact search for autocomplete pickers. While `enabled`, it
 * loads the first page of contacts and re-queries (debounced) as `setSearch`
 * is fed the input text. Disabling it clears the search and the options.
 */
export function useContactSearch(enabled: boolean, { excludeId, includeArchived }: ContactSearchOptions = {}) {
  const [search, setSearch] = useState('');
  const debouncedSearch = useDebouncedValue(search, SEARCH_DEBOUNCE_MS);
  const [options, setOptions] = useState<Contact[]>([]);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (!enabled) {
      setSearch('');
      setOptions([]);
      setLoading(false);
      return;
    }
    // A slower response for an earlier query must not replace a newer one.
    let cancelled = false;
    setLoading(true);
    getContacts({ limit: SEARCH_LIMIT, search: debouncedSearch, includeArchived })
      .then((response) => {
        if (!cancelled) setOptions((response.contacts || []).filter((c) => c.ID !== excludeId));
      })
      .catch((err) => {
        if (!cancelled) handleFetchError(err, 'loading contacts');
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [enabled, debouncedSearch, excludeId, includeArchived]);

  return { options, loading, search, setSearch };
}
