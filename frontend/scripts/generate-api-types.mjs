// Generates src/types/generated/api.ts from the backend's OpenAPI document
// (docs/openapi.json). Run `yarn openapi` (or `make openapi` from the root, which
// also regenerates the document). src/types/generated/api.test.ts fails while the
// committed file is stale.
import { readFileSync, writeFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import openapiTS, { astToString, COMMENT_HEADER } from 'openapi-typescript';

export const specUrl = new URL('../../docs/openapi.json', import.meta.url);
export const outputUrl = new URL('../src/types/generated/api.ts', import.meta.url);

export async function generateApiTypes(specText = readFileSync(specUrl, 'utf8')) {
  const ast = await openapiTS(JSON.parse(specText));
  return COMMENT_HEADER + astToString(ast);
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  writeFileSync(outputUrl, await generateApiTypes());
}
