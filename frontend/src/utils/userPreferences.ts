import i18n from '../i18n/config';
import { initializeDateFormatFromBackend } from '../DateFormatProvider';

// Applies the preferences the server stores for the user, right after login
// (password or OIDC), so they take effect without a reload.
export function applyUserPreferences(preferences: { language?: string; date_format?: string }): void {
  if (preferences.language && preferences.language !== i18n.language) {
    i18n.changeLanguage(preferences.language);
  }
  initializeDateFormatFromBackend(preferences.date_format);
}
