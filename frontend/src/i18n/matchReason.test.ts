import { describe, expect, test } from 'vitest';
import en from './locales/en.json';
import de from './locales/de.json';
import fr from './locales/fr.json';
import es from './locales/es.json';
import it from './locales/it.json';
import zh from './locales/zh.json';

// Every value the backend's duplicate detector puts in match_reason
// (MatchReason in backend/services/import_service.go). Both import dialogs
// translate it with contacts.import.preview.matchReason.<reason>.
const MATCH_REASONS = ['email', 'name', 'phone', 'vcard_uid'] as const;

describe('duplicate match reasons', () => {
  test.each(Object.entries({ en, de, fr, es, it, zh }))('%s translates every reason', (_locale, messages) => {
    const reasons: Record<string, string> = messages.contacts.import.preview.matchReason;
    for (const reason of MATCH_REASONS) {
      expect(reasons[reason], reason).toBeTruthy();
    }
  });
});
