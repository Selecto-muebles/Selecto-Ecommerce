import { execFile, execFileSync, spawn } from 'node:child_process';
import { randomBytes } from 'node:crypto';
import { mkdtempSync, writeFileSync } from 'node:fs';
import { createServer } from 'node:http';
import { createServer as createNetServer } from 'node:net';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { assertLoopback, assertOwnedContainer, quote } from './safety.mjs';
import { runScenarios } from './scenarios.mjs';
import { restoreLab } from './restore.mjs';
import { certifyNginx } from './nginx.mjs';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '../..');
const nonce = randomBytes(8).toString('hex');
const container = `selecto-cert-${nonce}`;
const password = randomBytes(24).toString('hex');
const jwt = randomBytes(32).toString('hex'), secret = randomBytes(32).toString('hex');
const database = { container, name: 'selecto_cert' };
const artifacts = mkdtempSync(join(tmpdir(), 'selecto-cert-'));
const binary = join(artifacts, 'api');
const docker = (args, options = {}) => {
  try { return execFileSync('docker', args, { encoding: 'utf8', stdio: ['pipe','pipe','pipe'], maxBuffer: 64 * 1024 * 1024, ...options }); }
  catch { throw new Error('Docker laboratory operation failed (arguments redacted)'); }
};
const sql = (query, db = database.name, role = 'selecto_lab') => docker(['exec', '-i', '-e', `PGPASSWORD=${password}`, container,
  'psql', '-X', '-v', 'ON_ERROR_STOP=1', '-At', '-h', '127.0.0.1', '-U', role, '-d', db], { input: query }).trim();
const execute = (file, args, env = process.env, timeout = 300000) => new Promise((resolveRun, reject) => {
  execFile(file, args, { cwd: root, env, timeout, maxBuffer: 32 * 1024 * 1024 }, (error, stdout, stderr) => {
    if (error) { writeFileSync(join(artifacts, `${file.split('/').pop()}-failure.log`), stdout + stderr, { mode: 0o600 }); reject(new Error(`${file} failed; private diagnostic in ${artifacts}`)); }
    else resolveRun(stdout);
  });
});
const pause = ms => new Promise(resolvePause => setTimeout(resolvePause, ms));
let owned = false, api, payments;
let baseEnv;
const payloads = [];
async function freePort() {
  const server = createNetServer();
  await new Promise(resolveListen => server.listen(0, '127.0.0.1', resolveListen));
  const port = server.address().port;
  await new Promise(resolveClose => server.close(resolveClose));
  return port;
}
function connection(db = database.name) {
  return `postgres://selecto_lab:${password}@127.0.0.1:${database.port}/${db}?sslmode=disable&search_path=commerce`;
}
function runAudit(db) {
  execFileSync(binary, ['database', 'audit'], { env: { ...baseEnv, DATABASE_URL: connection(db) }, stdio: 'pipe' });
}
try {
  docker(['run', '-d', '--name', container, '--label', `selecto.cert.owner=${nonce}`, '--tmpfs', '/var/lib/postgresql/data:rw,size=512m',
    '-p', '127.0.0.1::5432', '-e', 'POSTGRES_USER=postgres', '-e', `POSTGRES_PASSWORD=${password}`, '-e', 'POSTGRES_DB=selecto_cert', 'postgres:17-alpine']);
  owned = true;
  database.port = docker(['port', container, '5432/tcp']).trim().split(':').at(-1);
  let ready = false;
  for (let i = 0; i < 80; i++) {
    try { sql('SELECT 1', database.name, 'postgres'); ready = true; break; } catch { await pause(250); }
  }
  if (!ready) throw new Error('isolated PostgreSQL not ready');
  sql(`CREATE ROLE selecto_lab LOGIN PASSWORD ${quote(password)};
    ALTER DATABASE selecto_cert OWNER TO selecto_lab;
    CREATE SCHEMA commerce AUTHORIZATION selecto_lab;
    CREATE SCHEMA payments; REVOKE ALL ON SCHEMA payments FROM PUBLIC;
    ALTER ROLE selecto_lab IN DATABASE selecto_cert SET search_path=commerce,public;
    CREATE EXTENSION pgcrypto WITH SCHEMA public;`, database.name, 'postgres');
  baseEnv = { ...process.env, APP_ENV: 'development', GIN_MODE: 'release', DATABASE_URL: connection(), DB_SCHEMA: 'commerce', JWT_SECRET: jwt,
    INTERNAL_WEBHOOK_SECRET: secret, GOOGLE_CLIENT_ID: '', SMTP_HOST: '', SMTP_FROM: '', SMTP_PASSWORD: '',
    BREVO_API_KEY: '', BREVO_WEBHOOK_TOKEN: '', BREVO_CONTACTS_ENABLED: 'false', EMAIL_TASKS_ENABLED: 'false', RUN_EMBEDDED_WORKERS: 'false',
    RATE_LIMIT_PER_MINUTE: '100000', PAYMENTS_ID_TOKEN_AUDIENCE: '', DB_MAX_CONNS: '20' };
  process.stdout.write('Building actual API; migrating twice with restricted role...\n');
  await execute('go', ['build', '-o', binary, './cmd/api'], baseEnv);
  await execute(binary, ['database', 'migrate'], baseEnv);
  await execute(binary, ['database', 'migrate'], baseEnv);
  runAudit();
  process.stdout.write('Running race suite against isolated PostgreSQL...\n');
  const goOutput = await execute('go', ['test', '-race', '-v', '-count=1', '-coverprofile=' + join(artifacts, 'coverage.out'), './...'], { ...baseEnv, TEST_DATABASE_URL: connection() }, 600000);
  writeFileSync(join(artifacts, 'go-tests.log'), goOutput, { mode: 0o600 });
  const { verifySigned } = await import('./scenarios.mjs');
  payments = createServer(async (request, response) => {
    if (request.method === 'GET' && /^\/checkout\/[0-9]+$/.test(request.url)) {
      response.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' }).end('<!doctype html><html lang="es"><title>Pago simulado de laboratorio</title><h1>Pago simulado</h1><p>No es una conexión real con Mobbex ni se cobran fondos.</p></html>');
      return;
    }
    if (request.method !== 'POST' || request.url !== '/create-preference') { response.writeHead(404).end(); return; }
    let raw = ''; for await (const chunk of request) raw += chunk;
    if (!verifySigned(request.headers, raw, secret)) { response.writeHead(401).end(); return; }
    const payload = JSON.parse(raw); payloads.push(payload);
    response.writeHead(201, { 'Content-Type': 'application/json' }).end(JSON.stringify({ preference_id: `lab-${payload.order_id}`,
      checkout_url: `http://127.0.0.1:${payments.address().port}/checkout/${payload.order_id}`, environment: 'test' }));
  });
  await new Promise(resolveListen => payments.listen(0, '127.0.0.1', resolveListen));
  const port = await freePort(), apiURL = `http://127.0.0.1:${port}`;
  assertLoopback(apiURL);
  baseEnv = { ...baseEnv, PORT: String(port), PAYMENTS_SERVICE_URL: `http://127.0.0.1:${payments.address().port}`, STOREFRONT_URL: apiURL, ADMIN_URL: apiURL,
    CORS_ALLOWED_ORIGINS: 'http://127.0.0.1:4175,http://localhost:4175,http://127.0.0.1:4176,http://localhost:4176' };
  let apiLog = '';
  api = spawn(binary, [], { env: baseEnv, stdio: ['ignore', 'pipe', 'pipe'] });
  api.stdout.on('data', chunk => { apiLog += chunk; }); api.stderr.on('data', chunk => { apiLog += chunk; });
  api.on('error', error => { apiLog += error.message; });
  let healthy = false;
  for (let i = 0; i < 80; i++) {
    try { const response = await fetch(`${apiURL}/health`, { signal: AbortSignal.timeout(1000) }); if (response.ok) { healthy = true; break; } } catch { /* startup */ }
    await pause(250);
  }
  if (!healthy) { writeFileSync(join(artifacts, 'api-startup.log'), apiLog, { mode: 0o600 }); throw new Error('isolated API not healthy'); }
  process.stdout.write('Testing normal login, orders, stock, permissions and controlled load...\n');
  const result = await runScenarios({ apiURL, secret, sql, password, payloads });
  if (process.env.LAB_ADMIN_DIR || process.env.LAB_STOREFRONT_DIR) {
    if (!process.env.LAB_ADMIN_DIR || !process.env.LAB_STOREFRONT_DIR) throw new Error('Both UI directories required for full browser certification');
    await execute(process.execPath, [join(resolve(process.env.LAB_ADMIN_DIR), 'scripts/certification/live-browser.mjs')], {
      ...process.env, LAB_API_URL: apiURL, LAB_ADMIN_PASSWORD: password, LAB_CUSTOMER_PASSWORD: result.fixture.password,
      LAB_CUSTOMER_EMAIL: result.fixture.email, LAB_PRODUCT_ID: result.fixture.productID, LAB_ARTIFACTS: artifacts,
    }, 600000);
    result.browser = 'actual UI + HTTP API + PostgreSQL, normal login, no route mocks';
    result.nginx = await certifyNginx(resolve(process.env.LAB_STOREFRONT_DIR), apiURL, result.fixture.productID, artifacts);
  }
  const exited = new Promise(resolveExit => api.once('exit', resolveExit)); api.kill('SIGTERM'); await exited; api = undefined;
  await new Promise(resolveClose => payments.close(resolveClose)); payments = undefined;
  process.stdout.write('Restoring local synthetic dump into a second database...\n');
  const restoration = restoreLab({ docker, sql, runAudit, database, password });
  delete result.fixture;
  const report = { timestamp: new Date().toISOString(), scope: 'isolated synthetic laboratory; no production writes, emails or real gateway payments', ...result, restoration };
  writeFileSync(join(artifacts, 'report.json'), JSON.stringify(report, null, 2) + '\n', { mode: 0o600 });
  process.stdout.write(`PASS — evidence: ${artifacts}/report.json\n`);
} catch (error) {
  // Never print child environments, tokens or raw response bodies.
  process.stderr.write(`FAIL — ${error.message}\n`); process.exitCode = 1;
} finally {
  if (api) { const done = new Promise(resolveExit => api.once('exit', resolveExit)); api.kill('SIGTERM'); await Promise.race([done, pause(5000)]); if (api.exitCode === null) api.kill('SIGKILL'); }
  if (payments) await new Promise(resolveClose => payments.close(resolveClose));
  if (owned) {
    const label = docker(['inspect', '--format', '{{index .Config.Labels "selecto.cert.owner"}}', container]).trim();
    assertOwnedContainer(container, label, nonce); docker(['rm', '-f', container]);
    process.stdout.write('Removed only owned tmpfs PostgreSQL laboratory container.\n');
  }
}
