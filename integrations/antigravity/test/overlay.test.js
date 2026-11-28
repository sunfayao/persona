import assert from 'node:assert/strict'
import { closeSync, lstatSync, mkdirSync, mkdtempSync, openSync, readFileSync, readdirSync, rmSync, symlinkSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { test } from 'node:test'
import { OVERLAY, readOverlay, writeOverlay } from '../lib/overlay.js'

const stamp = '2026-01-01T00:00:00.123456Z'
const cut = { limit: 1, first: { step_index: 1, type: 'PLANNER_RESPONSE', created_at: stamp } }
const hidden = [{ step_index: 0, created_at: stamp }]

function fixture(t) {
  const root = mkdtempSync(join(tmpdir(), 'agy-overlay-'))
  t.after(() => rmSync(root, { recursive: true, force: true }))
  const brain = join(root, 'brain', '11111111-2222-4333-8444-555555555555')
  return { root, brain, file: join(brain, OVERLAY) }
}

test('overlay swaps complete private files atomically and removes an empty overlay', (t) => {
  const { brain, file } = fixture(t)
  writeOverlay(brain, { cut })
  if (process.platform !== 'win32') assert.equal(lstatSync(file).mode & 0o777, 0o600)
  const previous = openSync(file, 'r')
  try {
    writeOverlay(brain, { hidden })
    assert.deepEqual(JSON.parse(readFileSync(previous, 'utf8')), { version: 1, cut })
    assert.deepEqual(JSON.parse(readFileSync(file, 'utf8')), { version: 1, hidden })
  } finally {
    closeSync(previous)
  }
  assert.deepEqual(readdirSync(brain), [OVERLAY])
  writeOverlay(brain, {})
  assert.deepEqual(readdirSync(brain), [])
})

test('an overlay path or brain directory symlink cannot overwrite another file', { skip: process.platform === 'win32' }, (t) => {
  const { root, brain, file } = fixture(t)
  const other = join(root, 'elsewhere')
  mkdirSync(other)
  const unrelated = join(other, OVERLAY)
  const original = JSON.stringify({ version: 1, hidden })
  writeFileSync(unrelated, original)
  mkdirSync(brain, { recursive: true })
  symlinkSync(unrelated, file)
  assert.deepEqual(readOverlay(brain), {})
  writeOverlay(brain, { cut })
  assert.deepEqual(readOverlay(brain), { cut })
  assert.equal(readFileSync(unrelated, 'utf8'), original)

  rmSync(brain, { recursive: true })
  symlinkSync(other, brain, 'dir')
  assert.deepEqual(readOverlay(brain), {})
  assert.throws(() => writeOverlay(brain, { cut }))
  writeOverlay(brain, {})
  assert.equal(readFileSync(unrelated, 'utf8'), original)
})

test('unknown keys are ignored and an invalid overlay is absent', (t) => {
  const { brain, file } = fixture(t)
  mkdirSync(brain, { recursive: true })
  writeFileSync(file, JSON.stringify({ version: 1, hidden, future_key: { value: 1 } }))
  assert.deepEqual(readOverlay(brain), { hidden })
  for (const invalid of ['{', '{"version":2}', '{"version":1,"hidden":[{"step_index":0,"created_at":"yesterday"}]}']) {
    writeFileSync(file, invalid)
    assert.deepEqual(readOverlay(brain), {})
  }
})
