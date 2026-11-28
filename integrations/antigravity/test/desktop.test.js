import assert from 'node:assert/strict'
import { mkdtempSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { test } from 'node:test'
import { setTimeout as delay } from 'node:timers/promises'
import { startFakeNode } from '../../runtime-sdk/testing/fake-node.js'
import { createDesktopRuntime } from '../lib/desktop.js'

test('desktop names its own transcript root and routes only its controls', async t => {
  const dir = mkdtempSync(join(tmpdir(), 'desktop-agy-'))
  t.after(() => rmSync(dir, { recursive: true, force: true }))
  const key = '11111111-2222-4333-8444-555555555555'
  const session_id = 'desktop-agy-' + key
  const node = await startFakeNode(t)
  const calls = []
  const cascadeConfig = { plannerConfig: { planModel: "test-model" } }
  const runtime = createDesktopRuntime({ dir, socketPath: node.path, discover: async () => ({}),
    call: async (_, method, params) => {
      if (method === 'GetAllCascadeTrajectories') return { trajectorySummaries: { [key]: {
        summary: 'test session', lastModifiedTime: new Date().toISOString(), stepCount: 1,
      } } }
      if (method === 'GetCascadeTrajectorySteps') return { steps: [{ userInput: { userConfig: cascadeConfig } }] }
      calls.push({ method, params })
      return {}
    } })
  t.after(() => runtime.stop())
  await runtime.start()
  await node.ready
  const until = async fn => {
    for (let i = 0; i < 100 && !fn(); i++) await delay(25)
    assert.ok(fn())
  }
  await until(() => node.last('runtime.sessions'))
  const row = node.last('runtime.sessions').sessions[0]
  assert.equal(row.session_id, session_id)
  assert.equal(row.agy_conversation, key)
  assert.equal(row.agy_root, 'desktop')
  assert.deepEqual(await node.peer.call('session.items', { session_id }), { items: [], history: 'agy-node', to_seq: 0 })
  await node.peer.call('session.watch', { session_id, transcript: true })
  assert.equal(node.hello.caps.item, undefined)
  assert.equal(node.hello.caps.image, undefined)
  assert.deepEqual(node.all('session.items'), [])
  await node.peer.call('session.send', { session_id, text: 'next', client_msg_id: 'one' })
  await node.peer.call('session.send', { session_id, text: 'next', client_msg_id: 'one' })
  await node.peer.call('session.interrupt', { session_id })
  await assert.rejects(node.peer.call('session.send', { session_id: 'agy-' + key, text: 'wrong' }), { code: 'not_found' })
  assert.deepEqual(calls, [
    { method: 'SendUserCascadeMessage', params: { cascadeId: key, items: [{ text: 'next' }], cascadeConfig, clientMessageId: 'one' } },
    { method: 'CancelCascadeInvocation', params: { cascadeId: key } },
  ])
})
