// The one place that reads the clock. Everything else calls now() so that the time is
// easy to control in tests and to find (docs/development/frontend.md).
export function now(): Date {
  // eslint-disable-next-line no-restricted-syntax -- the single allowed clock read
  return new Date();
}
