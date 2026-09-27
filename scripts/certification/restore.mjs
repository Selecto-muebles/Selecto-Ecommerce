import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';

// Only the database in the newly owned, ephemeral laboratory container is used.
// This is not a recovery certification of Cloud SQL or of production data.
export function restoreLab({ docker, sql, runAudit, database, password }) {
  const restored = 'selecto_cert_restore';
  sql(`CREATE DATABASE ${restored} OWNER selecto_lab`, 'postgres', 'postgres');
  const dump = docker(['exec', '-e', `PGPASSWORD=${password}`, database.container,
    'pg_dump', '-h', '127.0.0.1', '-U', 'selecto_lab', '-d', database.name,
    '--format=custom', '--no-owner', '--no-acl', '--schema=commerce'], { encoding: null });
  docker(['exec', '-i', '-e', `PGPASSWORD=${password}`, database.container,
    'pg_restore', '-h', '127.0.0.1', '-U', 'selecto_lab', '-d', restored,
    '--no-owner', '--no-privileges', '--exit-on-error'], { input: dump });
  const tables = sql("SELECT tablename FROM pg_tables WHERE schemaname='commerce' ORDER BY tablename").split('\n').filter(Boolean);
  let rows = 0;
  const checksums = {};
  for (const table of tables) {
    assert.match(table, /^[a-z_]+$/);
    const query = `SELECT COALESCE(jsonb_agg(row_json ORDER BY row_json::text),'[]'::jsonb)::text FROM (SELECT to_jsonb(t) row_json FROM commerce.${table} t) data`;
    const original = sql(query), copy = sql(query, restored);
    const digest = value => createHash('sha256').update(value).digest('hex');
    assert.equal(digest(copy), digest(original), `restore differs for ${table}`);
    rows += JSON.parse(original).length;
    checksums[table] = digest(original);
  }
  const sequenceQuery = `SELECT sequencename,last_value FROM pg_sequences WHERE schemaname='commerce' ORDER BY sequencename`;
  assert.equal(sql(sequenceQuery, restored), sql(sequenceQuery), 'restored sequences differ');
  runAudit(restored);
  return { scope: 'synthetic local backup; NOT Cloud SQL production', tables: tables.length, rows, checksums, bytes: dump.length };
}
