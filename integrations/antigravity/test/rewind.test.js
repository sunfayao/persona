import assert from 'node:assert/strict'
import { appendFileSync, chmodSync, existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { get } from 'node:http'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { DatabaseSync } from 'node:sqlite'
import { test } from 'node:test'
import { fileURLToPath } from 'node:url'
import { startFakeNode, until } from '../../runtime-sdk/testing/fake-node.js'
import { createAgyRuntime } from '../lib/runtime.js'
import { OVERLAY, readOverlay } from '../lib/overlay.js'

const RAW = '11111111-2222-4333-8444-555555555555'
const SESSION = `agy-${RAW}`
const FAKE = fileURLToPath(new URL('./fake-acp-rewind.mjs', import.meta.url))
const at = '2026-01-01T00:00:00Z'
const cut = (limit) => ({ limit, first: { step_index: limit, type: records[limit].type, created_at: records[limit].created_at } })
const records = [
  { step_index: 0, type: 'USER_INPUT', content: 'Repeat' },
  { step_index: 1, type: 'PLANNER_RESPONSE', content: 'First answer' },
  { step_index: 2, type: 'USER_INPUT', content: 'Repeat' },
  { step_index: 3, type: 'PLANNER_RESPONSE', content: 'Second answer' },
].map((step) => ({ ...step, status: 'DONE', created_at: at }))
const nativeRecord = (step) => ({
  type: `CORTEX_STEP_TYPE_${step.type}`,
  metadata: { source: step.type === 'USER_INPUT' ? 'CORTEX_STEP_SOURCE_USER_EXPLICIT' : 'MODEL', createdAt: step.created_at },
  ...(step.type === 'USER_INPUT' ? { userInput: { items: [{ text: step.content }] } } : {}),
  ...(step.type === 'PLANNER_RESPONSE' ? { plannerResponse: { response: step.content } } : {}),
})

async function fixture(t, extra = {}) {
  const home = mkdtempSync(join(tmpdir(), 'agy-rewind-'))
  const config = join(home, 'config')
  const logs = join(config, 'brain', RAW, '.system_generated/logs')
  mkdirSync(logs, { recursive: true })
  const transcript = join(logs, 'transcript_full.jsonl')
  writeFileSync(transcript, records.map((r) => JSON.stringify(r)).join('\n') + '\n')
  writeFileSync(join(home, 'calls.jsonl'), '')
  const nativeFile = join(home, 'trajectory.json')
  const snapshot = {
    status: 'CASCADE_RUN_STATUS_IDLE', trajectory: { cascadeId: RAW, steps: records.map(nativeRecord) }, ...extra,
  }
  writeFileSync(nativeFile, JSON.stringify(snapshot))
  const db = new DatabaseSync(join(config, 'conversation_summaries.db'))
  db.exec('CREATE TABLE conversation_summaries (conversation_id TEXT PRIMARY KEY, status TEXT, not_fully_idle INTEGER, killed INTEGER, step_count INTEGER, title TEXT, workspace_uris TEXT, last_modified_time TEXT)')
  db.prepare('INSERT INTO conversation_summaries VALUES (?, ?, 0, 0, 4, ?, ?, ?)').run(RAW, 'CASCADE_RUN_STATUS_IDLE', 'Rewind example', JSON.stringify([`file://${home}`]), new Date().toISOString())
  const binary = join(home, 'agy')
  writeFileSync(binary, `#!/bin/sh\nexec '${process.execPath}' '${FAKE}' '${home}' "$@"\n`)
  chmodSync(binary, 0o700)
  const node = await startFakeNode(t)
  const options = { home, agyDir: config, agyBin: binary, socketPath: node.path, log: () => {} }
  let runtime = createAgyRuntime(options)
  t.after(async () => { await runtime.stop(); db.close(); rmSync(home, { recursive: true, force: true }) })
  runtime.start()
  await node.ready
  await until('rewind inventory', () => node.last('runtime.sessions')?.sessions.length)
  const calls = () => readFileSync(join(home, 'calls.jsonl'), 'utf8').split('\n').filter(Boolean).map(JSON.parse)
  const rewind = (extra = {}) => node.peer.call('session.rewind', { session_id: SESSION, prompt: 'Repeat', occurrence: 1, prompts_after: 0, restore: 'conversation', ...extra })
  const restart = async () => {
    await runtime.stop()
    const before = node.all('runtime.sessions').length
    runtime = createAgyRuntime(options)
    runtime.start()
    await until('restarted inventory', () => node.all('runtime.sessions').length > before)
  }
  const brain = join(config, 'brain', RAW)
  const overlay = () => readOverlay(brain)
  return { node, runtime, home, transcript, db, nativeFile, calls, rewind, restart, overlay, overlayPath: join(brain, OVERLAY) }
}

test('ACP updates during rewind cannot start autonomous work or restore the old tail', async (t) => {
  const { node, home, rewind, runtime, overlay } = await fixture(t, { holdRevert: true })
  await node.peer.call('session.watch', { session_id: SESSION, transcript: true })
  const pending = rewind()
  try {
    await until('rewind updates delivered', () => node.last('runtime.sessions')?.sessions[0]?.title === 'Rewind in progress')
    assert.notEqual(node.last('runtime.sessions').sessions[0].status, 'working')
    assert.ok(!node.all('session.delta').some((delta) => delta.text === 'Stale reply.'))
  } finally {
    writeFileSync(join(home, 'release-revert'), '')
    await pending
  }
  assert.equal(runtime.idle(), true)
  assert.deepEqual(overlay(), { cut: cut(2) })
  assert.deepEqual(node.all('session.items'), [])
})

test('the native fake answers port discovery and keeps serving the conversation', async (t) => {
  const { home, rewind, overlay } = await fixture(t)
  await rewind()
  await until('native service port', () => existsSync(join(home, 'service-port')))
  const port = Number(readFileSync(join(home, 'service-port'), 'utf8'))
  const probe = await new Promise((resolve) => {
    const req = get({ hostname: '127.0.0.1', port, path: '/', headers: { Connection: 'close', 'User-Agent': 'persona-ports' } }, (res) => {
      res.resume()
      res.on('end', () => resolve({ status: res.statusCode }))
    })
    req.on('error', (error) => resolve({ error }))
  })
  assert.equal(probe.error, undefined)
  assert.equal(probe.status, 404)
  assert.deepEqual(overlay(), { cut: cut(2) })
})

test('native rewind preserves the prefix through a cut and removes it once the export replaces the fingerprint', async (t) => {
  const { node, transcript, rewind, calls, db, overlay, overlayPath } = await fixture(t)
  assert.equal(node.hello.caps.rewind, true)
  await node.peer.call('session.watch', { session_id: SESSION, transcript: true })
  assert.equal((await rewind()).input_cleared, true)
  const native = calls().find((c) => c.method === 'RevertToCascadeStep')
  assert.deepEqual(native.params, { cascadeId: RAW, stepIndex: 1, conversationOnly: true, suppressErrorStep: true })
  assert.deepEqual(overlay(), { cut: cut(2) })
  const result = await rewind({ occurrence: 1, prompts_after: 0 })
  assert.equal(result.prompt, 'Repeat')
  assert.deepEqual(overlay(), { cut: cut(0) })
  appendFileSync(transcript, [
    { step_index: 0, type: 'USER_INPUT', content: 'New branch', created_at: new Date(Date.now() + 10).toISOString(), status: 'DONE' },
    { step_index: 1, type: 'PLANNER_RESPONSE', content: 'New answer', created_at: new Date(Date.now() + 10).toISOString(), status: 'DONE' },
  ].map((s) => JSON.stringify(s)).join('\n') + '\n')
  db.exec('UPDATE conversation_summaries SET step_count = 2')
  await node.peer.call('session.rename', { session_id: SESSION, title: 'Replacement' })
  await until('replacement clears the cut', () => !existsSync(overlayPath))
  assert.deepEqual(node.all('session.items'), [])
})

test('moved prompts and native busy state refuse before mutation', async (t) => {
  const { rewind, calls, nativeFile } = await fixture(t)
  await assert.rejects(rewind({ occurrence: 2, prompts_after: 0 }), (e) => e.code === 'rewind_mismatch')
  await assert.rejects(rewind({ restore: 'code_and_conversation' }), (e) => e.code === 'rewind_unsupported')
  const snapshot = JSON.parse(readFileSync(nativeFile, 'utf8'))
  writeFileSync(nativeFile, JSON.stringify({ ...snapshot, status: 'CASCADE_RUN_STATUS_RUNNING' }))
  await assert.rejects(rewind(), (e) => e.code === 'rewind_busy')
  assert.equal(calls().filter((c) => c.method === 'RevertToCascadeStep').length, 0)
})

test('a native failure preserves visible history and remains retryable', async (t) => {
  const { overlay, rewind, nativeFile } = await fixture(t, { fail: true })
  const initial = overlay()
  await assert.rejects(rewind(), /Native rewind refused/)
  const after = overlay()
  assert.deepEqual(after, initial)
  const snapshot = JSON.parse(readFileSync(nativeFile, 'utf8'))
  writeFileSync(nativeFile, JSON.stringify({ ...snapshot, fail: false }))
  assert.equal((await rewind()).input_cleared, true)
})

test('plan-mode messages match their original native input', async (t) => {
  const { rewind, transcript } = await fixture(t)
  const plan = records.map((step) => step.step_index === 2 ? { ...step, content: '/plan Repeat' } : step)
  writeFileSync(transcript, plan.map((s) => JSON.stringify(s)).join('\n') + '\n')
  assert.equal((await rewind({ prompt: '/plan Repeat' })).input_cleared, true)
})

test('a companion restart verifies a native cut whose timestamp precedes the abandoned export', async (t) => {
  const { rewind, db, restart, overlay } = await fixture(t)
  await rewind()
  db.prepare('UPDATE conversation_summaries SET last_modified_time = ?').run('2025-01-01T00:00:00Z')
  await restart()
  assert.deepEqual(overlay(), { cut: cut(2) })
})

test('a companion restart preserves an empty native trajectory with no steps field', async (t) => {
  const { rewind, db, restart, overlay } = await fixture(t)
  await rewind({ occurrence: 2, prompts_after: 1 })
  db.prepare('UPDATE conversation_summaries SET last_modified_time = ?').run('2025-01-01T00:00:00Z')
  await restart()
  assert.deepEqual(overlay(), { cut: cut(0) })
})

test('an inventory refresh revalidates an existing cut after native work advances', async (t) => {
  const { node, transcript, rewind, db, nativeFile, calls, overlay } = await fixture(t)
  const snapshot = JSON.parse(readFileSync(nativeFile, 'utf8'))
  const fullTrajectory = [...snapshot.trajectory.steps,
    nativeRecord({ ...records[2], step_index: 4, content: 'New question' }),
    nativeRecord({ ...records[3], step_index: 5, content: 'Recovered answer' })]
  await rewind()
  await until('rewound inventory', () => node.last('runtime.sessions')?.sessions[0]?.last_text === 'First answer')

  appendFileSync(transcript, [
    { ...records[2], step_index: 4, content: 'New question' },
    { ...records[3], step_index: 5, content: 'Recovered answer' },
  ].map((step) => JSON.stringify(step)).join('\n') + '\n')
  writeFileSync(nativeFile, JSON.stringify({ ...snapshot, trajectory: { ...snapshot.trajectory, steps: fullTrajectory } }))
  db.prepare('UPDATE conversation_summaries SET step_count = 6, last_modified_time = ?').run(new Date().toISOString())
  const checks = calls().filter((call) => call.method === 'GetCascadeTrajectory').length
  await node.peer.call('session.rename', { session_id: SESSION, title: 'Renamed' })
  await until('recovered last reply in the inventory', () => node.last('runtime.sessions')?.sessions[0]?.last_text === 'Recovered answer')
  assert.equal(calls().filter((call) => call.method === 'GetCascadeTrajectory').length, checks + 1)
  assert.deepEqual(overlay(), {})
})

test('a changed summary cannot expose the abandoned tail while the native trajectory is still rewound', async (t) => {
  const { node, transcript, rewind, db, nativeFile, calls, overlay } = await fixture(t)
  await rewind()
  await until('rewound inventory', () => node.last('runtime.sessions')?.sessions[0]?.last_text === 'First answer')
  appendFileSync(transcript, [
    { ...records[2], step_index: 4, content: 'Unexported question' },
    { ...records[3], step_index: 5, content: 'Abandoned answer' },
  ].map((step) => JSON.stringify(step)).join('\n') + '\n')
  db.prepare('UPDATE conversation_summaries SET step_count = 6, last_modified_time = ?').run(new Date().toISOString())
  const snapshot = JSON.parse(readFileSync(nativeFile, 'utf8'))
  assert.equal(snapshot.trajectory.steps.length, 2)
  const checks = calls().filter((call) => call.method === 'GetCascadeTrajectory').length
  const before = node.all('runtime.sessions').length
  await node.peer.call('session.rename', { session_id: SESSION, title: 'Renamed' })
  await until('inventory refreshed', () => node.all('runtime.sessions').length > before)
  assert.deepEqual(overlay(), { cut: cut(2) })
  assert.equal(calls().filter((call) => call.method === 'GetCascadeTrajectory').length, checks + 1)
  assert.equal(node.last('runtime.sessions').sessions[0].last_text, 'First answer')
})

test('native progress cannot clear a cut until the replacement export catches up', async (t) => {
  const { node, transcript, rewind, db, nativeFile, overlay } = await fixture(t)
  const snapshot = JSON.parse(readFileSync(nativeFile, 'utf8'))
  const replacement = [
    records[0], records[1],
    { ...records[2], content: 'Replacement question', created_at: '2026-01-02T00:00:00Z' },
    { ...records[3], content: 'Replacement answer', created_at: '2026-01-02T00:00:01Z' },
  ]
  await rewind()
  await until('rewound inventory', () => node.last('runtime.sessions')?.sessions[0]?.last_text === 'First answer')
  writeFileSync(nativeFile, JSON.stringify({ ...snapshot, trajectory: { ...snapshot.trajectory, steps: replacement.map(nativeRecord) } }))
  db.prepare('UPDATE conversation_summaries SET step_count = 4, last_modified_time = ?').run(new Date().toISOString())
  const before = node.all('runtime.sessions').length
  await node.peer.call('session.rename', { session_id: SESSION, title: 'Renamed' })
  await until('inventory refreshed', () => node.all('runtime.sessions').length > before)
  assert.deepEqual(overlay(), { cut: cut(2) })
  assert.equal(node.last('runtime.sessions').sessions[0].last_text, 'First answer')

  writeFileSync(transcript, replacement.map((step) => JSON.stringify(step)).join('\n') + '\n')
  await node.peer.call('session.rename', { session_id: SESSION, title: 'Renamed again' })
  await until('replacement export is visible', () => node.last('runtime.sessions')?.sessions[0]?.last_text === 'Replacement answer')
  assert.deepEqual(overlay(), {})
})

test('a stale native check cannot erase a cut installed by a newer rewind', async (t) => {
  const { node, home, transcript, rewind, db, nativeFile, overlay } = await fixture(t)
  await rewind()
  await until('first rewound inventory', () => node.last('runtime.sessions')?.sessions[0]?.last_text === 'First answer')
  await node.peer.call('session.watch', { session_id: SESSION, transcript: true })
  const abandoned = [
    { ...records[2], step_index: 4, content: 'Abandoned question' },
    { ...records[3], step_index: 5, content: 'Abandoned answer' },
  ]
  appendFileSync(transcript, abandoned.map((step) => JSON.stringify(step)).join('\n') + '\n')
  const snapshot = JSON.parse(readFileSync(nativeFile, 'utf8'))
  writeFileSync(nativeFile, JSON.stringify({ ...snapshot, holdTrajectoryOnce: true,
    trajectory: { ...snapshot.trajectory, steps: [...records.map(nativeRecord), ...abandoned.map(nativeRecord)] } }))
  db.prepare('UPDATE conversation_summaries SET step_count = 6, last_modified_time = ?').run(new Date().toISOString())
  await node.peer.call('session.rename', { session_id: SESSION, title: 'Start stale check' })
  await until('stale native check is held', () => existsSync(join(home, 'trajectory-held')))

  await rewind({ occurrence: 1, prompts_after: 1 })
  await until('second rewound inventory', () => node.last('runtime.sessions')?.sessions[0]?.last_text === 'First answer')
  writeFileSync(join(home, 'release-trajectory'), '')
  await until('stale native response is sent', () => existsSync(join(home, 'trajectory-response-sent')))
  await new Promise((resolve) => setTimeout(resolve, 100))
  assert.deepEqual(node.all('session.items'), [])
  assert.equal(node.all('runtime.sessions').some((event) => event.sessions?.[0]?.last_text === 'Abandoned answer'), false)
  assert.deepEqual(overlay(), { cut: cut(2) })
  assert.equal(node.last('runtime.sessions').sessions[0].last_text, 'First answer')
})

test('a stale check cannot publish while a newer rewind is installing its cut', async (t) => {
  const { node, home, transcript, rewind, db, nativeFile, overlay } = await fixture(t)
  await rewind()
  await until('first rewound inventory', () => node.last('runtime.sessions')?.sessions[0]?.last_text === 'First answer')
  await node.peer.call('session.watch', { session_id: SESSION, transcript: true })
  const abandoned = [
    { ...records[2], step_index: 4, content: 'Abandoned question' },
    { ...records[3], step_index: 5, content: 'Abandoned answer' },
  ]
  appendFileSync(transcript, abandoned.map((step) => JSON.stringify(step)).join('\n') + '\n')
  const snapshot = JSON.parse(readFileSync(nativeFile, 'utf8'))
  writeFileSync(nativeFile, JSON.stringify({ ...snapshot, holdTrajectoryOnce: true,
    trajectory: { ...snapshot.trajectory, steps: [...records.map(nativeRecord), ...abandoned.map(nativeRecord)] } }))
  db.prepare('UPDATE conversation_summaries SET step_count = 6, last_modified_time = ?').run(new Date().toISOString())
  await node.peer.call('session.rename', { session_id: SESSION, title: 'Start stale check' })
  await until('stale native check is held', () => existsSync(join(home, 'trajectory-held')))

  const armed = JSON.parse(readFileSync(nativeFile, 'utf8'))
  writeFileSync(nativeFile, JSON.stringify({ ...armed, holdAfterRevertOnce: true }))
  const second = rewind({ occurrence: 1, prompts_after: 1 })
  await until('second rewind is installing its cut', () => existsSync(join(home, 'after-trajectory-held')))
  writeFileSync(join(home, 'release-trajectory'), '')
  await until('stale native response is sent', () => existsSync(join(home, 'trajectory-response-sent')))
  await new Promise((resolve) => setTimeout(resolve, 100))
  writeFileSync(join(home, 'release-after-trajectory'), '')
  await second
  assert.deepEqual(node.all('session.items'), [])
  assert.equal(node.all('runtime.sessions').some((event) => event.sessions?.[0]?.last_text === 'Abandoned answer'), false)
  assert.deepEqual(overlay(), { cut: cut(2) })
  assert.equal(node.last('runtime.sessions').sessions[0].last_text, 'First answer')
})

test('row keeps its control-plane model through rewind and never sends transcript token fields', async (t) => {
  const { node, transcript, rewind } = await fixture(t)
  const seeded = [
    records[0],
    {
      ...records[1],
      model: 'gemini-4-carbon-high',
      input_tokens: 10000,
      cache_read_tokens: 2000,
      output_tokens: 150,
    },
    records[2],
    {
      ...records[3],
      model: 'claude-opus-5-5-high',
      input_tokens: 5000,
      cache_read_tokens: 40000,
      cache_write_tokens: 500,
      output_tokens: 400,
      context_window: 1000000,
    },
  ]
  writeFileSync(transcript, seeded.map((step) => JSON.stringify(step)).join('\n') + '\n')
  const row = () => node.last('runtime.sessions')?.sessions.find((s) => s.session_id === SESSION)
  await node.peer.call('session.rename', { session_id: SESSION, title: 'Usage before rewind' })
  await until('latest native model published', () => row()?.model === 'claude-opus-5-5-high')
  const contentFields = ['context_tokens', 'context_window', 'output_tokens', 'background', 'background_ended', 'plan', 'loop', 'facets']
  assert.ok(contentFields.every((key) => !Object.hasOwn(row(), key)))
  await rewind()
  await until('the rewound row keeps the earlier model', () => row()?.model === 'gemini-4-carbon-high')
  assert.ok(contentFields.every((key) => !Object.hasOwn(row(), key)))
  await rewind({ occurrence: 1, prompts_after: 0 })
  await until('no stale model survives a full rewind', () => row()?.model === undefined)
})
