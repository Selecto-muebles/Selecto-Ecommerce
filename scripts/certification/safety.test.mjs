import test from 'node:test';
import assert from 'node:assert/strict';
import { assertLoopback, assertOwnedContainer, quote } from './safety.mjs';
test('load and browser targets cannot be production', () => {
  for (const url of ['https://selectosport.com', 'http://api.selectosport.com', 'http://127.0.0.1.evil.invalid', 'http://user:secret@localhost:8080']) assert.throws(() => assertLoopback(url));
  assert.equal(assertLoopback('http://127.0.0.1:8080').hostname, '127.0.0.1');
});
test('cleanup requires exact local ownership', () => {
  const owner = '1234567890abcdef';
  assertOwnedContainer(`selecto-cert-${owner}`, owner, owner);
  for (const name of ['selecto-postgres-1', '/', 'selecto-cert-*']) assert.throws(() => assertOwnedContainer(name, owner, owner));
  assert.throws(() => assertOwnedContainer(`selecto-cert-${owner}`, 'someone-else', owner));
  assert.equal(quote("O'Reilly"), "'O''Reilly'");
});
