import assert from 'node:assert/strict';
import { randomBytes } from 'node:crypto';
import { execFileSync } from 'node:child_process';
import { resolve } from 'node:path';
import { readFileSync, writeFileSync } from 'node:fs';
import { createServer } from 'node:net';
import { assertLoopback } from './safety.mjs';

export async function certifyNginx(frontDir, apiURL, productID, artifacts) {
  const api = assertLoopback(apiURL), results = [];
  const nonce = randomBytes(8).toString('hex');
  const image = 'nginxinc/nginx-unprivileged:1.27-alpine@sha256:65e3e85dbaed8ba248841d9d58a899b6197106c23cb0ff1a132b7bfe0547e4c0';
  const docker = args => execFileSync('docker', args, { encoding: 'utf8', timeout: 30000 });
  const reservation = createServer();
  await new Promise(done => reservation.listen(0, '127.0.0.1', done));
  const port = reservation.address().port;
  await new Promise(done => reservation.close(done));
  const template = resolve(artifacts, 'nginx-certification.template');
  writeFileSync(template, readFileSync(resolve(frontDir, 'ops/nginx/default.conf.template'), 'utf8').replace('listen 8080;', `listen ${port};`));
  const origin = `http://127.0.0.1:${port}`;
  for (const unavailable of [false, true]) {
    const name = `selecto-seo-cert-${nonce}-${unavailable ? 'fallback' : 'live'}`;
    let owned = false;
    try {
      docker(['run', '-d', '--name', name, '--label', `selecto.cert.owner=${nonce}`, '--network', 'host',
        '-e', `ECOMMERCE_UPSTREAM=${unavailable ? 'http://127.0.0.1:1' : api.origin}`,
        '-v', `${resolve(frontDir, 'dist')}:/usr/share/nginx/html:ro`,
        '-v', `${template}:/etc/nginx/templates/default.conf.template:ro`, image]);
      owned = true;
      let ready = false;
      for (let i = 0; i < 50; i++) {
        try { if ((await fetch(origin + '/healthz', { signal: AbortSignal.timeout(1000) })).ok) { ready = true; break; } } catch { /* startup */ }
        await new Promise(done => setTimeout(done, 100));
      }
      if (!ready) throw new Error('Owned nginx certification runtime not ready');
      docker(['exec', name, 'nginx', '-t']);
      for (const path of unavailable ? ['/'] : ['/', '/productos/' + productID, '/?category=laboratorio', '/restablecer-contrasena?token=DO-NOT-REFLECT']) {
        const response = await fetch(origin + path); const body = await response.text();
        assert.equal(response.status, 200); assert.equal((body.match(/<title>/g) || []).length, 1, 'one initial title');
        assert.ok(body.includes('id="root"'), 'application shell present');
        assert.ok(!body.includes('<!--#'), 'SSI processed server-side');
        assert.ok(!body.includes('DO-NOT-REFLECT'), 'no recovery token reflected');
        if (unavailable) assert.match(body, /content="noindex,nofollow"/);
        else if (path.startsWith('/productos/')) assert.ok(body.includes('Equipo de certificación | Selecto'), 'product metadata exists before JS');
        else if (path === '/') assert.ok(body.includes('<title>Equipamiento Deportivo | Selecto</title>'), 'uptime matcher preserved');
      }
      if (!unavailable) {
        const xml = await fetch(origin + '/sitemap.xml'); assert.equal(xml.status, 200);
        assert.match(xml.headers.get('content-type'), /xml/); assert.ok((await xml.text()).includes('/productos/' + productID));
        assert.equal((await fetch(origin + '/_seo')).status, 404, 'internal SSI endpoint not public');
      }
      results.push(unavailable ? 'safe app shell/noindex when API unavailable' : 'initial product/category HTML + dynamic sitemap through nginx');
    } finally {
      if (owned) {
        const owner = docker(['inspect', '--format', '{{index .Config.Labels "selecto.cert.owner"}}', name]).trim();
        assert.equal(owner, nonce); docker(['rm', '-f', name]);
      }
    }
  }
  return results;
}
