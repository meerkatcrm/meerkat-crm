// navigator.clipboard only exists in secure contexts (HTTPS or localhost), so on a
// plain-HTTP install it is undefined. Resolves to whether the text was copied.
export async function copyToClipboard(text: string): Promise<boolean> {
  try {
    if (!navigator.clipboard?.writeText) {
      return false;
    }
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    return false;
  }
}
