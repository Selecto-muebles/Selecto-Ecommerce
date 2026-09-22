import { execFileSync } from 'node:child_process';
import { writeFile } from 'node:fs/promises';
import { pathToFileURL } from 'node:url';
import { PROJECT, OWNER, uptimeConfigs, alertPolicies } from './monitoring_config.mjs';

export function safeTarget(resources, desired) {
  const matches = resources.filter(r => r.userLabels?.managed_by === OWNER && r.userLabels?.selecto_key === desired.userLabels.selecto_key);
  if (matches.length > 1) throw new Error(`Duplicados administrados: ${desired.displayName}`);
  const current = matches[0];
  if (resources.some(r => r.displayName === desired.displayName && r.name !== current?.name)) throw new Error(`Nombre ocupado por recurso ajeno: ${desired.displayName}`);
  return current;
}
// Responses add server defaults and condition IDs. Compare only desired fields.
export function containsDesired(actual, desired) {
  if (Array.isArray(desired)) return Array.isArray(actual) && actual.length === desired.length && desired.every((x,i) => containsDesired(actual[i],x));
  if (desired && typeof desired === 'object') return !!actual && Object.entries(desired).every(([k,v]) =>
    // Protobuf omits a zero threshold in successful API responses.
    (k==='thresholdValue' && v===0 && actual[k]===undefined) || containsDesired(actual[k],v));
  return actual === desired;
}

export async function reconcile(api, email, apply = false) {
  if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email ?? '')) throw new Error('Indicar --email con el destinatario confirmado.');
  const channels = await api.list('notificationChannels');
  const channel = channels.find(c => c.type === 'email' && c.labels?.email_address?.toLowerCase() === email.toLowerCase() && c.enabled !== false);
  if (!channel) throw new Error('Crear/verificar un canal email habilitado para el destinatario en Monitoring antes de aplicar.');
  if (channel.verificationStatus === 'UNVERIFIED') throw new Error('Verificar el canal antes de activar alertas.');
  const existingChecks = await api.list('uptimeCheckConfigs');
  const existingPolicies = await api.list('alertPolicies');
  const plannedChecks = uptimeConfigs().map(c => ({ ...c, name: safeTarget(existingChecks,c)?.name ?? `projects/${PROJECT}/uptimeCheckConfigs/pendiente-${c.userLabels.selecto_key}` }));
  const plannedPolicies = alertPolicies(plannedChecks, channel.name);
  // Validate the entire plan before the first mutation; never adopt foreign resources.
  for (const desired of plannedPolicies) safeTarget(existingPolicies, desired);
  if (!apply) return { mode:'plan', project:PROJECT, channel:channel.name, uptimeChecks:plannedChecks, policies:plannedPolicies };
  const changes = [];
  async function upsert(collection, resources, desired) {
    const current = safeTarget(resources, desired);
    if (current && containsDesired(current,desired)) return current;
    const path = current ? `${current.name}?updateMask=${Object.keys(desired).join(',')}` : `projects/${PROJECT}/${collection}`;
    // Do not retry POST blindly on lost responses. Ownership labels allow a
    // subsequent plan to discover the existing ID without creating duplicates.
    const result = await api.request(path, current ? 'PATCH':'POST', current ? {...desired,name:current.name}:desired);
    changes.push({collection,name:result.name,before:current ?? null});
    return result;
  }
  try {
    const checks = [];
    for (const desired of uptimeConfigs()) checks.push(await upsert('uptimeCheckConfigs',existingChecks,desired));
    for (const desired of alertPolicies(checks,channel.name)) await upsert('alertPolicies',existingPolicies,desired);
    return {mode:'applied',project:PROJECT,channel:channel.name,changes};
  } catch (e) { e.changes = changes; throw e; }
}

function client() {
  let token;
  try { token = execFileSync(process.env.GCLOUD_BIN || 'gcloud',['auth','print-access-token'],{encoding:'utf8',timeout:30000}).trim(); }
  catch { throw new Error('No se pudo obtener la credencial local de gcloud.'); }
  async function request(path, method='GET', body) {
    const response = await fetch(`https://monitoring.googleapis.com/v3/${path}`,{
      method,headers:{Authorization:`Bearer ${token}`,'Content-Type':'application/json'},
      body:body ? JSON.stringify(body):undefined,signal:AbortSignal.timeout(30000),
    });
    const data = await response.json();
    if (!response.ok) throw new Error(`Monitoring ${method}: ${response.status} ${data.error?.message ?? ''}`);
    return data;
  }
  return {request,async list(collection) {
    const items=[]; let pageToken;
    do {
      const query = new URLSearchParams({pageSize:'100',...(pageToken ? {pageToken}:{})});
      const data = await request(`projects/${PROJECT}/${collection}?${query}`);
      items.push(...(data[collection] ?? [])); pageToken=data.nextPageToken;
    } while (pageToken);
    return items;
  }};
}

async function main() {
  const args=process.argv.slice(2);
  const value=flag=>args.find(a=>a.startsWith(flag+'='))?.slice(flag.length+1);
  if (args.some(a=>!a.startsWith('--email=')&&!a.startsWith('--receipt=')&&a!=='--apply')) throw new Error('Uso: --email=CORREO [--apply --receipt=/tmp/archivo.json]');
  const receipt=value('--receipt');
  if (args.includes('--apply')&&!receipt) throw new Error('--apply requiere --receipt para guardar IDs y estado previo.');
  // Check the receipt path before any API mutation and never overwrite an old receipt.
  if (receipt) await writeFile(receipt,'{}',{mode:0o600,flag:'wx'});
  try {
    const result=await reconcile(client(),value('--email'),args.includes('--apply'));
    if (receipt) await writeFile(receipt,JSON.stringify(result,null,2));
    console.log(JSON.stringify(result,null,2));
  } catch(e) {
    if (receipt) await writeFile(receipt,JSON.stringify({mode:'partial',changes:e.changes ?? [],error:e.message},null,2));
    throw e;
  }
}
if (process.argv[1] && import.meta.url===pathToFileURL(process.argv[1]).href) main().catch(e=>{console.error(e.message);process.exitCode=1;});
