import assert from 'node:assert/strict'
import { appendFileSync, chmodSync, existsSync, mkdirSync, mkdtempSync, readFileSync, readlinkSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { DatabaseSync } from 'node:sqlite'
import { test } from 'node:test'
import { fileURLToPath } from 'node:url'
import { packageHash } from '../../runtime-sdk/lib/package.js'
import { startFakeNode, until } from '../../runtime-sdk/testing/fake-node.js'
import { PACKAGE_NAME } from '../install.mjs'
import { createAgyRuntime } from '../lib/runtime.js'
import { AgyAcpClient } from '../lib/acp.js'

const FAKE = fileURLToPath(new URL('./fake-acp-stream.mjs', import.meta.url))
const RAW = '11111111-2222-4333-8444-555555555555'
const SESSION = `agy-${RAW}`

async function fixture(t, options = {}, startParams = {}) {
  const directory = mkdtempSync(join(tmpdir(), 'agy-stream-'))
  writeFileSync(join(directory, 'updates.jsonl'), '')
  writeFileSync(join(directory, 'calls.jsonl'), '')
  const binary = join(directory, 'agy')
  writeFileSync(binary, `#!/bin/sh\nexec '${process.execPath}' '${FAKE}' '${directory}' "$@"\n`)
  chmodSync(binary, 0o700)
  mkdirSync(join(directory, 'config/brain', RAW), { recursive: true })
  const node = await startFakeNode(t)
  const runtime = createAgyRuntime({ agyBin: binary, agyDir: join(directory, 'config'), home: directory, socketPath: node.path, log: () => {}, ...options })
  t.after(async () => { await runtime.stop(); rmSync(directory, { recursive: true, force: true }) })
  runtime.start()
  await node.ready
  await node.peer.call('session.start', { cwd: directory, ...startParams })
  await node.peer.call('session.watch', { session_id: SESSION, transcript: true })
  await until('initial session is listed', () => sessionStatus(node))
  const calls = () => readFileSync(join(directory, 'calls.jsonl'), 'utf8').split('\n').filter(Boolean).map(JSON.parse)
  const update = (value) => appendFileSync(join(directory, 'updates.jsonl'), `${JSON.stringify(value)}\n`)
  const chunk = async (text, expected = text) => {
    update({ sessionUpdate: 'agent_message_chunk', content: { type: 'text', text } })
    await until(`live text ${expected}`, () => node.last('session.delta')?.text === expected, 15_000)
  }
  const promptCount = (count) => until(`${count} prompts started`, () => calls().filter((r) => r.method === 'session/prompt').length === count, 15_000)
  return { node, directory, calls, update, chunk, promptCount, runtime }
}

test('AGY streams one assistant message at a time across tool calls', { timeout: 20_000 }, async (t) => {
  const { node, directory, update, chunk, promptCount } = await fixture(t, { exportPollMs: 20 })
  await node.peer.call('session.send', { session_id: SESSION, text: 'Make the change.' })
  await promptCount(1)
  await chunk('Preparing ')
  await chunk('the change.', 'Preparing the change.')
  update({ sessionUpdate: 'tool_call', toolCallId: 'tool-1', title: 'Read' })
  update({ sessionUpdate: 'tool_call_update', toolCallId: 'tool-1', status: 'completed' })
  await chunk('The change is ')
  await chunk('complete.', 'The change is complete.')
  update({ end: true })
  await until('the ACP turn is no longer working', () => sessionStatus(node) !== 'working')
  assert.equal(node.last('session.delta').text, 'The change is complete.')
  const partial = [userStep(0, 'Make the change.')]
  transcripts(directory, partial, partial)
  await node.peer.call('session.rename', { session_id: SESSION, title: 'Partial export' })
  await until('the partial export was read', () => node.last('runtime.sessions')?.sessions[0]?.title === 'Partial export')
  assert.equal(node.last('session.delta').text, 'The change is complete.')
  const complete = [...partial, replyStep(1, 'The change is complete.')]
  transcripts(directory, complete, complete)
  await until('only the full export retires live text', () => node.last('session.delta')?.text === '')
  assert.deepEqual(node.all('session.items'), [])
})

test('a queued prompt can replace the prior stream without an extra empty boundary', { timeout: 20_000 }, async (t) => {
  const { node, directory, update, chunk, promptCount } = await fixture(t, { exportPollMs: 20 })
  await node.peer.call('session.send', { session_id: SESSION, text: 'First request.' })
  await promptCount(1)
  await chunk('First reply')
  await node.peer.call('session.send', { session_id: SESSION, text: 'Second request.' })
  await chunk(' complete.', 'First reply complete.')
  update({ end: true })
  await promptCount(2)
  assert.equal(node.last('session.delta').text, 'First reply complete.')
  await chunk('Second reply.')
  update({ end: true })
  const steps = [userStep(0, 'First request.'), replyStep(1, 'First reply complete.'), userStep(2, 'Second request.'), replyStep(3, 'Second reply.')]
  transcripts(directory, steps, steps)
  await until('the second export clears the second reply', () => node.last('session.delta')?.text === '')
})

test('AGY completion retires the cached reply after the viewer leaves', { timeout: 20_000 }, async (t) => {
  const { node, directory, update, chunk, promptCount } = await fixture(t, { exportPollMs: 20 })
  await node.peer.call('session.send', { session_id: SESSION, text: 'First request.' })
  await promptCount(1)
  await chunk('Previous reply.')
  await node.peer.call('session.unwatch', { session_id: SESSION })
  update({ end: true })
  const steps = [userStep(0, 'First request.'), replyStep(1, 'Previous reply.')]
  transcripts(directory, steps, steps)
  await until('unwatched completion clears the reply', () => node.last('session.delta')?.text === '', 2000)
  await node.peer.call('session.watch', { session_id: SESSION, transcript: true })
  await node.peer.call('session.send', { session_id: SESSION, text: 'Second request.' })
  await promptCount(2)
  assert.equal(node.last('session.delta').text, '')
  await chunk('New reply.')
  update({ end: true })
})

test('an unexported assistant reply stays live until its settle deadline', async (t) => {
  const exportSettleTimeoutMs = 5000
  const { node, directory, update, chunk, promptCount, runtime } = await fixture(t, { exportPollMs: 20, exportSettleTimeoutMs }, { assistant: true })
  await node.peer.call('session.send', { session_id: SESSION, text: 'Show me options.', assistant: true })
  await promptCount(1)
  const reply = 'Choose an option:\n```persona-view\n{"title":"Next step"}\n```'
  await chunk(reply)
  let now = Date.now()
  t.mock.method(Date, 'now', () => now)
  update({ end: true })
  await until('ACP returned without an export', () => runtime.idle() && sessionStatus(node) === 'done')
  assert.equal(node.last('session.delta').text, reply)
  now += exportSettleTimeoutMs + 1
  await until('the timeout retires the text', () => node.last('session.delta')?.text === '')
  assert.equal(node.all('session.delta').filter(({ text }) => !text).length, 1)
  assert.deepEqual(node.all('session.items'), [])
  assert.equal(existsSync(join(directory, 'config', 'brain', RAW, '.persona-history-epoch')), false)
})

function transcripts(directory, compact, complete) {
  const logs = join(directory, 'config/brain', RAW, '.system_generated/logs')
  mkdirSync(logs, { recursive: true })
  for (const [file, rows] of [['transcript.jsonl', compact], ['transcript_full.jsonl', complete]]) {
    writeFileSync(join(logs, file), rows.map((row) => JSON.stringify(row)).join('\n') + '\n')
  }
}

const userStep = (index, text) => ({ step_index: index, source: 'USER_EXPLICIT', type: 'USER_INPUT', status: 'DONE', content: text })
const replyStep = (index, text) => ({ step_index: index, source: 'MODEL', type: 'PLANNER_RESPONSE', status: 'DONE', content: text })

function nativeState(directory, status, stepCount) {
  const db = new DatabaseSync(join(directory, 'config/conversation_summaries.db'))
  try {
    db.exec('CREATE TABLE IF NOT EXISTS conversation_summaries (conversation_id TEXT PRIMARY KEY, status TEXT, not_fully_idle INTEGER, killed INTEGER, step_count INTEGER, title TEXT, workspace_uris TEXT, last_modified_time TEXT)')
    db.prepare("INSERT OR REPLACE INTO conversation_summaries VALUES (?, ?, 0, 0, ?, '', '[]', '')")
      .run(RAW, `CASCADE_RUN_STATUS_${status}`, stepCount)
  } finally {
    db.close()
  }
}

const sessionStatus = (node) => node.last('runtime.sessions')?.sessions.find((session) => session.session_id === SESSION)?.status

test('autonomous wakes stream work and settle only once native work is idle and exported', async (t) => {
  const { node, directory, calls, update, chunk, runtime } = await fixture(t, { exportPollMs: 20 })
  const prior = [userStep(0, 'Run the workflow.'), replyStep(1, 'Workflow started.')]
  transcripts(directory, prior, prior)
  nativeState(directory, 'IDLE', 2)

  update({ sessionUpdate: 'agent_thought_chunk', content: { text: 'Inspect the result.' } })
  await until('autonomous working status before native activity', () => sessionStatus(node) === 'working', 1000)
  assert.deepEqual(node.all('session.items'), [])
  await chunk('Reading the result.')
  update({ sessionUpdate: 'tool_call', toolCallId: 'read', name: 'read_file' })
  update({ sessionUpdate: 'tool_call_update', toolCallId: 'read', status: 'completed' })
  await chunk('Workflow finished.')
  assert.equal(calls().filter((call) => call.method === 'session/prompt').length, 0)

  const wake = { step_index: 2, type: 'SYSTEM_MESSAGE', content: 'The workflow finished.' }
  const partial = [...prior, wake, replyStep(3, 'Reading the result.')]
  transcripts(directory, partial, partial)
  await new Promise((resolve) => setTimeout(resolve, 80))
  assert.equal(node.last('session.delta').text, 'Workflow finished.', 'an idle summary and a partial export do not settle the turn')

  const complete = [...partial, { ...replyStep(4, ''), thinking: 'Inspect the result.', tool_calls: [{ name: 'read_file', args: { Path: 'result.txt' } }] },
    { step_index: 5, type: 'GENERIC', status: 'DONE', content: 'Found it.' }, replyStep(6, 'Workflow finished.')]
  nativeState(directory, 'RUNNING', 7)
  transcripts(directory, complete, complete)
  await new Promise((resolve) => setTimeout(resolve, 80))
  assert.equal(node.last('session.delta').text, 'Workflow finished.', 'a running summary does not settle even with the reply exported')
  nativeState(directory, 'IDLE', 7)
  await until('native export replaces autonomous work', () => sessionStatus(node) === 'done' && node.last('session.delta')?.text === '' && runtime.idle())
  assert.deepEqual(node.all('session.items'), [])
})

test('autonomous wakes retire after inactivity without native export', async (t) => {
  const { node, directory, update, chunk, runtime } = await fixture(t, { autonomousIdleMs: 600, exportPollMs: 20, exportSettleTimeoutMs: 100 })
  nativeState(directory, 'IDLE', 0)
  update({ sessionUpdate: 'tool_call_update', toolCallId: 'read', name: 'read_file', status: 'completed', rawOutput: 'Found it.' })
  await until('a tool update can start the wake', () => sessionStatus(node) === 'working', 1000)
  await chunk('Still awake.')
  await until('inactivity retires the stream and working status', () => node.last('session.delta')?.text === '' && sessionStatus(node) !== 'working' && runtime.idle(), 1500)
  assert.deepEqual((await node.peer.call('session.items', { session_id: SESSION })).items, [])
  await new Promise((resolve) => setTimeout(resolve, 150))
  await chunk('Awake again.')
  const complete = [{ step_index: 0, type: 'SYSTEM_MESSAGE' }, replyStep(1, 'Awake again.')]
  transcripts(directory, complete, complete)
  nativeState(directory, 'IDLE', 2)
  await until('first normal export settles after the ephemeral wake expires', () => runtime.idle() && node.last('session.delta')?.text === '', 300)
})

for (const status of ['RUNNING', 'IDLE']) {
  test(`a wake resumes after a long tool pause with native status ${status}`, async (t) => {
    const { node, directory, update, chunk, runtime } = await fixture(t, { autonomousIdleMs: 600, exportPollMs: 20 })
    nativeState(directory, status, 0)
    update({ sessionUpdate: 'tool_call', toolCallId: 'slow-tool', name: 'run_command' })
    await until('slow tool is live', () => sessionStatus(node) === 'working')
    await until('stream retires during the tool pause', () => runtime.idle() && sessionStatus(node) !== 'working')
    if (status === 'IDLE') {
      update({ sessionUpdate: 'tool_call_update', toolCallId: 'slow-tool', status: 'completed', rawOutput: 'Finished.' })
      await until('resumed tool is complete', () => sessionStatus(node) === 'working')
    }
    await chunk('Command finished.')

    const complete = [{ step_index: 0, type: 'SYSTEM_MESSAGE' },
      { ...replyStep(1, ''), tool_calls: [{ name: 'run_command' }] },
      { step_index: 2, type: 'GENERIC', status: 'DONE', content: 'Finished.' }, replyStep(3, 'Command finished.')]
    transcripts(directory, complete, complete)
    nativeState(directory, 'IDLE', 4)
    await until('the single native wake export settles the resumed stream', () => runtime.idle() && node.last('session.delta')?.text === '', 300)
  })
}

test('an autonomous wake stays live while its permission card awaits an answer', async (t) => {
  const { node, calls, update, chunk, runtime } = await fixture(t, { autonomousIdleMs: 300, exportPollMs: 20 })
  const permissionId = 'wake-permission'
  const answer = () => calls().find((call) => call.id === permissionId)?.result?.outcome
  let answerCard
  node.resolve = () => new Promise((resolve) => { answerCard = resolve })
  await chunk('Waiting for permission.')
  update({ permission: permissionId })
  await until('permission card reaches the phone', () => answerCard)
  await new Promise((resolve) => setTimeout(resolve, 400))
  assert.equal(answer(), undefined, 'the wake must not cancel the card after its inactivity deadline')
  assert.equal(runtime.idle(), false)

  answerCard({ allow: true })
  await until('ACP permission answer', () => answer())
  assert.deepEqual(answer(), { outcome: 'selected', optionId: 'allow' })
  await new Promise((resolve) => setTimeout(resolve, 60))
  assert.equal(runtime.idle(), false, 'answering the card gives the wake a fresh inactivity window')
  await chunk('Continuing.', 'Waiting for permission.Continuing.')
  await until('wake settles after the answer and subsequent inactivity', () => runtime.idle() && node.last('session.delta')?.text === '')
})

test('empty updates cannot start an autonomous wake', async (t) => {
  const { node, update, runtime } = await fixture(t)
  const before = node.all('session.delta').length
  update({ sessionUpdate: 'agent_message_chunk', content: { text: ' \n ' } })
  update({ sessionUpdate: 'agent_thought_chunk', content: { text: ' \t ' } })
  update({ sessionUpdate: 'tool_call_update', status: 'completed' })
  update({ sessionUpdate: 'session_info_update', title: 'Updates delivered' })
  await until('ACP updates delivered', () => node.last('runtime.sessions')?.sessions[0]?.title === 'Updates delivered')
  assert.notEqual(sessionStatus(node), 'working')
  assert.equal(node.all('session.delta').length, before)
  assert.equal(runtime.idle(), true)
})

test('an installed update exits as soon as the autonomous wake retires', async (t) => {
  const store = mkdtempSync(join(tmpdir(), 'agy-wake-update-'))
  t.after(() => rmSync(store, { recursive: true, force: true }))
  const exits = []
  const { node, directory, chunk } = await fixture(t, {
    idleCheckMs: 10_000, retryMs: 20, exportPollMs: 20,
    update: { name: PACKAGE_NAME, hash: 'running', store, exit: (hash) => exits.push(hash) },
  })
  await chunk('Wake reply.')
  const files = [{ path: 'antigravity/companion.mjs', data: Buffer.from('export {}\n') }]
  const hash = packageHash(files)
  node.offer = { name: PACKAGE_NAME, hash, files: files.map((file) => ({ ...file, data: file.data.toString('base64') })) }
  node.peer.close(new Error('node upgraded'))
  await until('package installed while wake is active', () => {
    try { return readlinkSync(join(store, 'current')) === hash } catch { return false }
  })
  assert.deepEqual(exits, [])
  const steps = [replyStep(0, 'Wake reply.')]
  transcripts(directory, steps, steps)
  nativeState(directory, 'IDLE', 1)
  await until('wake retirement triggers exit without waiting for the idle poll', () => exits.length, 1500)
  assert.deepEqual(exits, [hash])
})

test('a user send takes over an autonomous wake without waiting for it', async (t) => {
  const { node, update, chunk, promptCount, runtime, directory } = await fixture(t, { exportPollMs: 20 })
  update({ sessionUpdate: 'tool_call', toolCallId: 'read', name: 'read_file' })
  await chunk('Background progress.')
  const sent = await node.peer.call('session.send', { session_id: SESSION, text: 'Next request.' })
  assert.notEqual(sent?.queued, true)
  await promptCount(1)
  await chunk('User reply.')
  update({ end: true })
  const steps = [userStep(0, 'Next request.'), replyStep(1, 'User reply.')]
  transcripts(directory, steps, steps)
  await until('user turn settles independently', () => node.last('session.delta')?.text === '' && sessionStatus(node) !== 'working' && runtime.idle())
  assert.deepEqual(node.all('session.items'), [])
})

test('a text-only wake can retire on export without writing a wake receipt', async (t) => {
  const { node, directory, chunk, runtime } = await fixture(t, { exportPollMs: 20 })
  const prior = [userStep(0, 'Start the task.'), replyStep(1, 'Task started.')]
  transcripts(directory, prior, prior)
  nativeState(directory, 'IDLE', 2)
  await chunk('Task finished.')
  const complete = [...prior, replyStep(2, 'Task finished.')]
  transcripts(directory, complete, complete)
  nativeState(directory, 'IDLE', 3)
  await until('the exported wake retires the stream', () => runtime.idle() && node.last('session.delta')?.text === '')
  assert.equal(existsSync(join(directory, 'config', 'brain', RAW, '.persona-observed-wakes')), false)
  assert.deepEqual(node.all('session.items'), [])
})

test('a policy-changing send cannot resume AGY during an autonomous wake', async (t) => {
  const { node, calls, chunk } = await fixture(t)
  await chunk('Background progress.')
  const resumes = calls().filter((call) => call.method === 'session/resume').length
  await assert.rejects(node.peer.call('session.send', {
    session_id: SESSION, text: 'Next request.', supervision_instructions: 'Ask before editing files.',
  }), /Wait for the current turn to finish before changing supervision policy/)
  assert.equal(calls().filter((call) => call.method === 'session/resume').length, resumes)
  assert.equal(calls().some((call) => call.method === 'session/prompt'), false)
  assert.equal(node.last('session.delta').text, 'Background progress.')
})

test('interrupt settles an autonomous wake even when the native summary still says running', async (t) => {
  const { node, directory, calls, update, chunk, runtime } = await fixture(t, { exportSettleTimeoutMs: 120, exportPollMs: 20 })
  nativeState(directory, 'RUNNING', 0)
  update({ sessionUpdate: 'tool_call', toolCallId: 'read', name: 'read_file' })
  await chunk('Background progress.')
  await node.peer.call('session.interrupt', { session_id: SESSION })
  await until('native cancel requested', () => calls().some((call) => call.method === 'session/cancel'))
  await until('interrupt retires autonomous work', () => node.last('session.delta')?.text === '' && sessionStatus(node) !== 'working' && runtime.idle())
  assert.deepEqual((await node.peer.call('session.items', { session_id: SESSION })).items, [])
})

test('a late export of one wake cannot settle the next wake', async (t) => {
  const { node, directory, update, chunk, runtime } = await fixture(t, { autonomousIdleMs: 1300, exportPollMs: 20, exportSettleTimeoutMs: 500 })
  const announce = (toolCallId) => update({ sessionUpdate: 'tool_call', toolCallId, name: 'read_file' })
  announce('first-read')
  await chunk('Workflow finished.')
  await until('first wake times out unexported', () => runtime.idle() && node.last('session.delta')?.text === '')

  announce('second-read')
  await chunk('Workflow finished.')
  const first = [{ step_index: 0, type: 'SYSTEM_MESSAGE' },
    { ...replyStep(1, ''), tool_calls: [{ name: 'read_file' }] }, replyStep(2, 'Workflow finished.')]
  transcripts(directory, first, first)
  nativeState(directory, 'IDLE', 3)
  await new Promise((resolve) => setTimeout(resolve, 550))
  assert.equal(runtime.idle(), false, 'expiry does not reassign the earlier wake with the same tool and reply')
  assert.equal(node.last('session.delta').text, 'Workflow finished.')

  const next = [...first, { step_index: 3, type: 'SYSTEM_MESSAGE' }, replyStep(4, 'Workflow finished.')]
  transcripts(directory, next, next)
  nativeState(directory, 'IDLE', 5)
  await new Promise((resolve) => setTimeout(resolve, 100))
  assert.equal(runtime.idle(), false, 'the second wake cannot borrow the first wake\'s tool call')

  const complete = [...next.slice(0, -1), { ...replyStep(4, ''), tool_calls: [{ name: 'read_file' }] }, replyStep(5, 'Workflow finished.')]
  transcripts(directory, complete, complete)
  nativeState(directory, 'IDLE', 6)
  await until('second wake settles with its own export', () => runtime.idle() && node.last('session.delta')?.text === '', 400)
  assert.deepEqual(node.all('session.items'), [])
})

for (const exportSettleTimeoutMs of [450, 1100]) {
  test(`a chained timeout protects the next wake with a ${exportSettleTimeoutMs} ms export window`, async (t) => {
    const { node, directory, chunk, runtime } = await fixture(t, { autonomousIdleMs: 700, exportPollMs: 20, exportSettleTimeoutMs })
    const reply = 'Workflow finished.'
    const steps = []
    const exportWake = () => {
      steps.push({ step_index: steps.length, type: 'SYSTEM_MESSAGE' }, replyStep(steps.length + 1, reply))
      transcripts(directory, steps, steps)
      nativeState(directory, 'IDLE', steps.length)
    }
    const retired = () => runtime.idle() && node.last('session.delta')?.text === ''

    await chunk(reply)
    await until('the first wake times out', retired)
    const firstExpiry = Date.now() + exportSettleTimeoutMs
    await chunk(reply)
    exportWake()
    await until('the second wake also times out', retired)
    await new Promise((resolve) => setTimeout(resolve, Math.max(0, firstExpiry - Date.now() + 30)))

    await chunk(reply)
    exportWake()
    await new Promise((resolve) => setTimeout(resolve, 100))
    assert.equal(runtime.idle(), false, 'the second wake export cannot settle the third wake')
    assert.equal(node.last('session.delta').text, reply)

    exportWake()
    await until('the third wake settles on its own export', retired, 350)
  })
}

test('a live wake discards only unobserved missed boundaries on expiry', async (t) => {
  const exportSettleTimeoutMs = 1100
  const { node, directory, chunk, runtime } = await fixture(t, { autonomousIdleMs: 800, exportPollMs: 20, exportSettleTimeoutMs })
  const reply = 'Workflow finished.'
  const retired = () => runtime.idle() && node.last('session.delta')?.text === ''

  await chunk(reply)
  await until('the first wake times out', retired)
  await chunk(reply)
  await until('the ephemeral second wake times out', retired)
  const expiresAt = Date.now() + exportSettleTimeoutMs

  await chunk(reply)
  const first = [{ step_index: 0, type: 'SYSTEM_MESSAGE' }, replyStep(1, reply)]
  transcripts(directory, first, first)
  nativeState(directory, 'IDLE', first.length)
  await new Promise((resolve) => setTimeout(resolve, 550))
  await chunk(' ', `${reply} `)
  await new Promise((resolve) => setTimeout(resolve, Math.max(0, expiresAt - Date.now() + 50)))
  assert.equal(runtime.idle(), false, 'expiry cannot reassign the first wake export')
  await chunk(' ', `${reply}  `)

  const complete = [...first, { step_index: 2, type: 'SYSTEM_MESSAGE' }, replyStep(3, reply)]
  transcripts(directory, complete, complete)
  nativeState(directory, 'IDLE', complete.length)
  await until('the third wake settles without an export for the second wake', retired, 350)
})

test('unknown and archived sessions cannot start autonomous work', async (t) => {
  const { node, update, chunk, runtime } = await fixture(t)
  const unknown = '66666666-2222-4333-8444-555555555555'
  await node.peer.call('session.watch', { session_id: `agy-${unknown}` })
  update({ sessionId: unknown, sessionUpdate: 'agent_message_chunk', content: { text: 'Unknown session.' } })
  await chunk('Known session.')
  assert.equal(node.all('session.delta').some((delta) => delta.text === 'Unknown session.'), false)
  await node.peer.call('session.close', { session_id: SESSION })
  await until('close retires autonomous work', () => node.last('session.delta')?.text === '' && runtime.idle() && !sessionStatus(node))

  update({ sessionUpdate: 'agent_message_chunk', content: { text: 'Archived session.' } })
  await new Promise((resolve) => setTimeout(resolve, 100))
  assert.equal(node.all('session.delta').some((delta) => delta.text === 'Archived session.'), false)
  assert.equal(runtime.idle(), true)
})

for (const partialTools of [false, true]) {
  test(`partial export preserves the final reply (tools: ${partialTools})`, async (t) => {
    const { node, directory, update, chunk, promptCount } = await fixture(t, { exportPollMs: 20 })
    await node.peer.call('session.send', { session_id: SESSION, text: 'Inspect.' })
    await promptCount(1)
    await chunk('Checking first.')
    update({ sessionUpdate: 'tool_call', toolCallId: 'read', name: 'read_file' })
    update({ sessionUpdate: 'tool_call_update', toolCallId: 'read', status: 'completed', rawOutput: 'Found it.' })
    await chunk('Final answer.')
    const partial = [userStep(0, 'Inspect.')]
    if (partialTools) partial.push(replyStep(1, 'Checking first.'),
      { ...replyStep(2, 'Checking a file.'), tool_calls: [{ name: 'read_file', args: {} }] },
      { step_index: 3, type: 'GENERIC', status: 'DONE', content: 'Found it.' })
    transcripts(directory, partial, partial)
    update({ end: true })
    await until('ACP turn ended', () => sessionStatus(node) !== 'working')
    assert.equal(node.last('session.delta').text, 'Final answer.')
    const full = [...partial, replyStep(partial.at(-1).step_index + 1, 'Final answer.')]
    transcripts(directory, full, full)
    await until('exported reply retires the stream', () => node.last('session.delta')?.text === '')
    assert.deepEqual(node.all('session.items'), [])
  })
}

test('queued sends acknowledge the queue and defer options until execution', async (t) => {
  const { node, calls, update, promptCount } = await fixture(t)
  await node.peer.call('session.send', { session_id: SESSION, text: 'First.' })
  await promptCount(1)
  const count = calls().length
  const result = await node.peer.call('session.send', { session_id: SESSION, text: 'Second.', model: 'next-model', mode: 'plan' })
  assert.equal(result.queued, true)
  assert.equal(calls().slice(count).some((call) => call.method === 'session/set_model' || call.method === 'session/set_mode'), false)
  update({ end: true })
  await promptCount(2)
  const nextCalls = calls().slice(count)
  assert.ok(nextCalls.some((call) => call.method === 'session/set_model'))
  assert.ok(nextCalls.some((call) => call.method === 'session/set_mode'))
  update({ end: true })
})

test('plan-mode starts advertise readonly support', async (t) => {
  const { node, directory, calls } = await fixture(t)
  assert.equal(node.hello.caps.readonly, true)
  const result = await node.peer.call('session.start', { cwd: directory, permission_mode: 'plan', readonly: true })
  assert.equal(result.session.permission_mode, 'plan')
  assert.ok(calls().some((call) => call.method === 'session/set_mode' && call.params.modeId === 'plan'))
})

for (const text of ['invalid-once', 'invalid-always']) {
  test(`stale sessions get one clean resume retry: ${text}`, async (t) => {
    const { node, calls, update, promptCount } = await fixture(t)
    const before = calls().length
    await node.peer.call('session.send', { session_id: SESSION, text })
    await promptCount(2)
    const recovery = calls().slice(before).filter((call) => ['session/prompt', 'session/resume'].includes(call.method))
    assert.deepEqual(recovery.map((call) => call.method), ['session/prompt', 'session/resume', 'session/prompt'])
    if (text === 'invalid-always') {
      await until('retry failure published', () => node.last('runtime.sessions')?.sessions.find((s) => s.session_id === SESSION)?.turn_error === 'Invalid session')
      assert.equal(calls().filter((call) => call.method === 'session/prompt').length, 2)
    } else {
      update({ end: true })
      await until('retried turn ends', () => node.last('runtime.sessions')?.sessions.find((s) => s.session_id === SESSION)?.status !== 'working')
    }
  })
}

for (const secondReply of ['Second answer.', 'First answer.']) {
  test(`a previous turn exported during its successor cannot replace ${secondReply}`, async (t) => {
    const { node, directory, update, chunk, promptCount } = await fixture(t)
    await node.peer.call('session.send', { session_id: SESSION, text: 'First.' })
    await promptCount(1)
    await chunk('First answer.')
    await node.peer.call('session.send', { session_id: SESSION, text: 'Second.' })
    update({ end: true })
    await promptCount(2)
    await chunk(secondReply)
    const first = [userStep(0, 'First.'), replyStep(1, 'First answer.')]
    transcripts(directory, first, first)
    update({ end: true })
    await until('second ACP turn ended', () => sessionStatus(node) !== 'working')
    assert.equal(node.last('session.delta').text, secondReply)
    const full = [...first, userStep(2, 'Second.'), replyStep(3, secondReply)]
    transcripts(directory, full, full)
    await until('second export clears the stream', () => node.last('session.delta')?.text === '')
    assert.deepEqual(node.all('session.items'), [])
  })
}

test('a rejected initial preparation leaves idle history and error state unchanged', async (t) => {
  const { node, calls } = await fixture(t)
  const before = await node.peer.call('session.items', { session_id: SESSION })
  const batches = node.all('session.items').length
  await assert.rejects(node.peer.call('session.send', { session_id: SESSION, text: 'Do not publish this.', model: 'invalid-model' }), /Unknown model/)
  const after = await node.peer.call('session.items', { session_id: SESSION })
  assert.deepEqual(after, before)
  const inventories = node.all('runtime.sessions').length
  await node.peer.call('session.rename', { session_id: SESSION, title: 'Checked' })
  await until('inventory refreshed', () => node.all('runtime.sessions').length > inventories)
  const row = node.last('runtime.sessions').sessions.find((row) => row.session_id === SESSION)
  assert.equal(row.status, 'idle')
  assert.equal(row.turn_error, undefined)
  assert.equal(node.all('session.items').slice(batches).flatMap((batch) => batch.items).some((item) => item.text === 'Do not publish this.'), false)
  assert.equal(calls().some((call) => call.method === 'session/prompt'), false)
})

test('preparation does not expose a user message before accepting the send', async (t) => {
  const { node } = await fixture(t)
  const before = await node.peer.call('session.items', { session_id: SESSION })
  let release, entered = false
  const gate = new Promise((resolve) => { release = resolve })
  const original = AgyAcpClient.prototype.setModel
  t.mock.method(AgyAcpClient.prototype, 'setModel', async function (...args) {
    entered = true
    await gate
    return original.apply(this, args)
  })
  const rejected = assert.rejects(node.peer.call('session.send', { session_id: SESSION, text: 'Not accepted.', model: 'invalid-model' }), /Unknown model/)
  try {
    await until('model preparation started', () => entered)
    assert.deepEqual(await node.peer.call('session.items', { session_id: SESSION }), before)
  } finally {
    release()
    await rejected
  }
})

test('a queued preparation failure remains visible after its queued acknowledgement', async (t) => {
  const { node, update, promptCount } = await fixture(t)
  await node.peer.call('session.send', { session_id: SESSION, text: 'First.' })
  await promptCount(1)
  assert.equal((await node.peer.call('session.send', { session_id: SESSION, text: 'Queued.', model: 'invalid-model' })).queued, true)
  update({ end: true })
  await until('queued failure published', () => node.last('runtime.sessions')?.sessions.find((row) => row.session_id === SESSION)?.turn_error?.includes('Unknown model'))
  const row = node.last('runtime.sessions').sessions.find((row) => row.session_id === SESSION)
  assert.notEqual(row.status, 'working')
})

for (const prompt of ['', 'Inspect the screenshot.']) {
  test(`start sends ${prompt ? 'text and an image' : 'an image-only prompt'} to ACP`, async (t) => {
    const data = 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aF8sAAAAASUVORK5CYII='
    const { node, calls, update, promptCount } = await fixture(t, {}, { prompt, images: [{ mime: 'image/png', data }] })
    await promptCount(1)
    const blocks = calls().find((call) => call.method === 'session/prompt').params.prompt
    assert.deepEqual(blocks, [...(prompt ? [{ type: 'text', text: prompt }] : []), { type: 'image', mimeType: 'image/png', data }])
    assert.deepEqual(node.all('session.items'), [])
    update({ end: true })
  })
}
