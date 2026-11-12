const API_SERVER_URL = import.meta.env.REACT_APP_API_URL || '';

/**
 * The API sends a thumbnail as a path ("/api/v1/contacts/5/profile_picture?thumbnail=true&v=..."),
 * which the browser caches by URL. When the API lives on another origin, prefix that origin.
 * Returns undefined for "no thumbnail" so it can go straight into an Avatar's src.
 */
export function thumbnailSrc(url: string | undefined | null): string | undefined {
  if (!url) return undefined;
  return url.startsWith('/') ? `${API_SERVER_URL}${url}` : url;
}
