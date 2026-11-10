interface ContactNameParts {
  prefix?: string | null;
  firstname?: string | null;
  middle_name?: string | null;
  lastname?: string | null;
  suffix?: string | null;
  nickname?: string | null;
}

interface ContactNameOptions {
  /** Insert the nickname in quotes after the first name: Anna "Annie" Schmidt. */
  nickname?: boolean;
  /** Include prefix, middle name and suffix as well. */
  full?: boolean;
}

/**
 * The one way to turn a contact into a display name. Empty parts are dropped,
 * so a contact without a last name gets no trailing or double space.
 */
export function contactDisplayName(
  contact: ContactNameParts,
  { nickname = false, full = false }: ContactNameOptions = {}
): string {
  return [
    full ? contact.prefix : '',
    contact.firstname,
    nickname && contact.nickname?.trim() ? `"${contact.nickname.trim()}"` : '',
    full ? contact.middle_name : '',
    contact.lastname,
    full ? contact.suffix : '',
  ]
    .map((part) => part?.trim())
    .filter(Boolean)
    .join(' ');
}
