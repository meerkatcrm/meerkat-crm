import { describe, expect, it } from 'vitest';
import committed from './api.ts?raw';
import spec from '../../../../docs/openapi.json?raw';
import { generateApiTypes } from '../../../scripts/generate-api-types.mjs';

describe('generated API types', () => {
  it('match docs/openapi.json (run `yarn openapi` when this fails)', async () => {
    expect(committed).toBe(await generateApiTypes(spec));
  });
});
