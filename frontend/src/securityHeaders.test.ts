import { describe, expect, test } from 'vitest';
// The security headers of the web app live in one nginx snippet that both deployment shapes
// include: the all-in-one image (docker/nginx.conf) and the separate frontend image
// (frontend/nginx.conf). These tests fail when a config drifts from that.
import snippet from '../security-headers.conf?raw';
import frontendNginx from '../nginx.conf?raw';
import frontendDockerfile from '../Dockerfile?raw';
import dockerNginx from '../../docker/nginx.conf?raw';
import rootDockerfile from '../../Dockerfile?raw';

const INCLUDE_LINE = 'include /etc/nginx/security-headers.conf;';

const REQUIRED_HEADERS = [
  'Content-Security-Policy',
  'X-Frame-Options',
  'X-Content-Type-Options',
  'Referrer-Policy',
  'Permissions-Policy',
  'Cross-Origin-Opener-Policy',
  'Cross-Origin-Resource-Policy',
  'Strict-Transport-Security',
];

const stripComments = (text: string) => text.replace(/#[^\n]*/g, '');

function parseSnippet(text: string): [string, string][] {
  const out: [string, string][] = [];
  for (const m of stripComments(text).matchAll(/add_header\s+(\S+)\s+("([^"]*)"|(\$\w+))\s+always\s*;/g)) {
    out.push([m[1] ?? '', m[3] ?? m[4] ?? '']);
  }
  return out;
}

function parseLocations(conf: string): { path: string; body: string }[] {
  const text = stripComments(conf);
  const locations: { path: string; body: string }[] = [];
  const re = /location\s+([^{]+?)\s*\{/g;
  let m: RegExpExecArray | null;
  while ((m = re.exec(text))) {
    let depth = 1;
    let i = re.lastIndex;
    while (depth > 0 && i < text.length) {
      if (text[i] === '{') depth++;
      else if (text[i] === '}') depth--;
      i++;
    }
    locations.push({ path: (m[1] ?? '').trim(), body: text.slice(re.lastIndex, i - 1) });
    re.lastIndex = i;
  }
  return locations;
}

describe('security-headers.conf', () => {
  const headers = parseSnippet(snippet);
  const csp = headers.find(([n]) => n === 'Content-Security-Policy')?.[1] ?? '';
  const directives = csp.split(';').map((d) => d.trim());
  const directive = (name: string) => directives.find((d) => d.startsWith(name + ' ')) ?? '';

  test.each(REQUIRED_HEADERS)('sets %s', (name) => {
    expect(headers.some(([n]) => n.toLowerCase() === name.toLowerCase())).toBe(true);
  });

  test('has a strict policy for the app', () => {
    for (const d of ["default-src 'self'", "object-src 'none'", "base-uri 'self'", "frame-ancestors 'none'"]) {
      expect(directives).toContain(d);
    }
    expect(directive('script-src')).toContain("'self'");
    expect(directive('script-src')).not.toMatch(/'unsafe-(inline|eval)'/);
  });
});

describe.each([
  ['docker/nginx.conf', dockerNginx],
  ['frontend/nginx.conf', frontendNginx],
])('%s', (_name, conf) => {
  const bare = stripComments(conf);

  test('adds no security header of its own', () => {
    const own = [...bare.matchAll(/add_header\s+(\S+)/g)].map((m) => m[1]?.toLowerCase());
    expect(own.filter((n) => n !== 'cache-control')).toEqual([]);
  });

  test('derives HSTS from the scheme the browser used', () => {
    expect(bare).toMatch(/map\s+\$http_x_forwarded_proto\s+\$forwarded_proto/);
    expect(bare).toMatch(/map\s+\$forwarded_proto\s+\$hsts_header/);
  });

  test('includes the snippet where it serves files, and only there', () => {
    const locations = parseLocations(conf);
    expect(locations.length).toBeGreaterThan(0);
    for (const { path, body } of locations) {
      const proxied = /proxy_pass\b/.test(body);
      const included = body.includes(INCLUDE_LINE);
      if (proxied) {
        // The backend sends its own, stricter headers; adding more would send them twice.
        expect(included, `${path} is proxied`).toBe(false);
        expect(body, `${path} is proxied`).not.toMatch(/\badd_header\b/);
      } else if (!/^\s*return\b/m.test(body)) {
        expect(included, `${path} serves files`).toBe(true);
      }
    }
  });
});

test.each([
  ['Dockerfile', rootDockerfile, 'frontend/security-headers.conf'],
  ['frontend/Dockerfile', frontendDockerfile, 'security-headers.conf'],
])('%s copies the snippet into the image', (_name, text, source) => {
  expect(text).toContain(`COPY ${source} /etc/nginx/security-headers.conf`);
});
