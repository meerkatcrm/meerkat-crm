// Extends Vitest's expect with jest-dom matchers (toBeInTheDocument, etc.)
import '@testing-library/jest-dom/vitest';
import { afterEach } from 'vitest';
import { cleanup } from '@testing-library/react';

// jsdom does not implement scrolling; App calls window.scrollTo on route changes
window.scrollTo = () => {};

// Vitest runs without globals, so Testing Library's automatic cleanup never
// registers. Unmount after every test; otherwise components and hooks left
// mounted can run scheduled React work after jsdom is torn down, failing CI
// with "ReferenceError: window is not defined" from an unrelated test file.
afterEach(() => {
  cleanup();
});
