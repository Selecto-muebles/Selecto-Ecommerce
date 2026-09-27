import { execFileSync } from 'node:child_process';
import { randomBytes } from 'node:crypto';
import { readFile, writeFile } from 'node:fs/promises';

// A one-off log policy, isolated from operational policies. Automatically
// removed after five minutes; it never forces production requests to fail.
const project = 'destry-development';
const email = 'maurilaganga@gmail.com';
const receipt = process.argv.find(arg => arg.startsWith('--receipt='))?.slice(10);
const cleanup = process.argv.includes('--cleanup');
if (!(process.argv.includes('--apply') || cleanup) || !receipt || !receipt.startsWith('/tmp/')) throw new Error('Use --apply --receipt=/tmp/unique-receipt.json or --cleanup --receipt=/tmp/existing-receipt.json');
const state = cleanup ? JSON.parse(await readFile(receipt, 'utf8')) : {
  started: new Date().toISOString(), scope: 'CONTROLLED NOTIFICATION; no production failure', nonce: randomBytes(8).toString('hex'), policy: null, removed: false,
};
const nonce = state.nonce;
if (!/^[a-f0-9]{16}$/.test(nonce)) throw new Error('Invalid ownership receipt');
if (cleanup && !new RegExp(`^projects/${project}/alertPolicies/[0-9]+$`).test(state.policy)) throw new Error('Invalid policy target');
if (!cleanup) await writeFile(receipt, '{}\n', { flag: 'wx', mode: 0o600 });
const token = execFileSync(process.env.GCLOUD_BIN || 'gcloud', ['auth', 'print-access-token'], { encoding: 'utf8', timeout: 30000 }).trim();
async function request(base, path, method = 'GET', body) {
  const response = await fetch(`${base}/${path}`, { method, headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' }, body: body ? JSON.stringify(body) : undefined, signal: AbortSignal.timeout(30000) });
  if (response.status === 204) return {};
  if (method === 'GET' && response.status === 404) return null;
  const data = await response.json();
  if (!response.ok) throw new Error(`GCP ${method}: ${response.status}`);
  return data;
}
const monitoring = (path, method, body) => request('https://monitoring.googleapis.com/v3', path, method, body);
const save = () => writeFile(receipt, JSON.stringify(state, null, 2) + '\n', { mode: 0o600 });
let policy;
try {
  if (cleanup) {
    policy = await monitoring(state.policy);
    if (!policy) { state.removed = true; await save(); }
  } else {
  const data = await monitoring(`projects/${project}/notificationChannels?pageSize=100`);
  const channel = data.notificationChannels?.find(item => item.type === 'email' && item.labels?.email_address === email && item.enabled !== false && item.verificationStatus !== 'UNVERIFIED');
  if (!channel) throw new Error('Confirmed operational channel not available');
  policy = await monitoring(`projects/${project}/alertPolicies`, 'POST', {
    displayName: `Selecto - PRUEBA CONTROLADA de notificación ${nonce}`,
    documentation: { content: 'Esta es una prueba controlada solicitada por Selecto. NO hay una caída ni un fallo de clientes. Confirma la recepción por el canal operativo.', mimeType: 'text/markdown' },
    enabled: true, combiner: 'OR', notificationChannels: [channel.name],
    userLabels: { managed_by: 'selecto-certification', drill_nonce: nonce },
    conditions: [{ displayName: 'Único evento sintético del ensayo', conditionMatchedLog: { filter: `logName="projects/${project}/logs/selecto-certification" AND jsonPayload.drill_nonce="${nonce}"` } }],
    alertStrategy: { notificationRateLimit: { period: '300s' }, autoClose: '1800s' },
  });
  state.policy = policy.name; state.channel = channel.name; await save();
  process.stdout.write(`Temporary notification policy ready. Receipt: ${receipt}\n`);
  await new Promise(resolve => setTimeout(resolve, 30000));
  await request('https://logging.googleapis.com/v2', 'entries:write', 'POST', {
    logName: `projects/${project}/logs/selecto-certification`, resource: { type: 'global', labels: { project_id: project } },
    entries: [{ severity: 'NOTICE', insertId: nonce, jsonPayload: { drill_nonce: nonce, message: 'PRUEBA CONTROLADA Selecto: sin incidencia productiva' } }],
  });
  state.eventWritten = new Date().toISOString(); await save();
  process.stdout.write('Test log accepted. Await email receipt; automatic cleanup in five minutes.\n');
  await new Promise(resolve => setTimeout(resolve, 300000));
  }
} finally {
  if (policy) {
    const current = await monitoring(policy.name);
    if (current?.userLabels?.managed_by !== 'selecto-certification' || current?.userLabels?.drill_nonce !== nonce) throw new Error('Refusing to remove an unowned policy');
    await monitoring(policy.name, 'DELETE');
    state.removed = true; state.finished = new Date().toISOString(); await save();
    process.stdout.write('Removed only temporary test policy; operational policies unchanged.\n');
  }
}
