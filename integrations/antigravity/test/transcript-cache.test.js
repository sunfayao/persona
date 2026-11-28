import assert from 'node:assert/strict'
import { appendFileSync, mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { describe, it } from 'node:test'
import { readTranscript } from '../lib/map.js'

function makeBrainDir() {
  const root = mkdtempSync(join(tmpdir(), 'agy-cache-'))
  const logs = join(root, '.system_generated', 'logs')
  mkdirSync(logs, { recursive: true })
  return { root, logs, file: join(logs, 'transcript_full.jsonl') }
}

function line(step) {
  return JSON.stringify(step) + '\n'
}

describe('incremental transcript caching', () => {
  it('caches active (!settled) transcripts and detects same-size fast rewrites via SHA-256', () => {
    const { root, file } = makeBrainDir()
    try {
      const s0 = { step_index: 0, type: 'USER_INPUT', source: 'USER_EXPLICIT', created_at: '2026-11-21T00:00:00Z', content: 'hello-aaa' }
      const s1 = { step_index: 1, type: 'PLANNER_RESPONSE', status: 'DONE', created_at: '2026-11-21T00:00:01Z', content: 'reply-aaa' }
      writeFileSync(file, line(s0) + line(s1))

      const first = readTranscript(root, true)
      const second = readTranscript(root, true)
      assert.equal(first.steps.length, 2)
      // Active (!settled) unchanged read hits cache and preserves object identity.
      assert.equal(second, first)
      assert.equal(second.steps[0], first.steps[0])

      // Same-size fast rewrite on the same inode within the 1s settle window must be detected.
      const s1b = { step_index: 1, type: 'PLANNER_RESPONSE', status: 'DONE', created_at: '2026-11-21T00:00:01Z', content: 'reply-bbb' }
      const rewritten = line(s0) + line(s1b)
      assert.equal(Buffer.byteLength(rewritten), Buffer.byteLength(line(s0) + line(s1)))
      writeFileSync(file, rewritten)

      const third = readTranscript(root, true)
      assert.notEqual(third, first)
      assert.equal(third.steps[1].content, 'reply-bbb')
      assert.ok(third.revision, 'same-size rewrite must advance revision')
    } finally {
      rmSync(root, { recursive: true, force: true })
    }
  })

  it('incrementally parses appended lines, partial trailing lines, rewinds, and late arrivals', () => {
    const { root, file } = makeBrainDir()
    try {
      const s0 = { step_index: 0, type: 'USER_INPUT', source: 'USER_EXPLICIT', created_at: '2026-11-21T00:00:00Z', content: 'start' }
      const s1 = { step_index: 1, type: 'PLANNER_RESPONSE', status: 'DONE', created_at: '2026-11-21T00:00:01Z', content: 'done one' }
      writeFileSync(file, line(s0) + line(s1))

      const t1 = readTranscript(root, true)
      assert.equal(t1.steps.length, 2)

      // Append a partial line without trailing newline first, then complete it.
      const s2Raw = JSON.stringify({ step_index: 2, type: 'PLANNER_RESPONSE', status: 'DONE', created_at: '2026-11-21T00:00:02Z', content: 'background update' })
      appendFileSync(file, s2Raw.slice(0, 20))
      const tPartial = readTranscript(root, true)
      assert.equal(tPartial.steps.length, 2)
      assert.equal(tPartial.steps[0], t1.steps[0])
      assert.equal(tPartial.steps[1], t1.steps[1])

      appendFileSync(file, s2Raw.slice(20) + '\n')
      const t2 = readTranscript(root, true)
      assert.equal(t2.steps.length, 3)
      // Earlier parsed steps kept by object identity via incremental append.
      assert.equal(t2.steps[0], t1.steps[0])
      assert.equal(t2.steps[1], t1.steps[1])
      assert.equal(t2.steps[2].content, 'background update')

      // Appended late-arriving step (index 3 skipped, 4 arrives, then 3 arrives).
      const s4 = { step_index: 4, type: 'GENERIC', status: 'DONE', created_at: '2026-11-21T00:00:04Z', content: 'second result' }
      appendFileSync(file, line(s4))
      const t4 = readTranscript(root, true)
      assert.equal(t4.steps.at(-1).step_index, 4)

      const s3Late = { step_index: 3, type: 'GENERIC', status: 'DONE', created_at: '2026-11-21T00:00:03Z', content: 'first result' }
      appendFileSync(file, line(s3Late))
      const tLate = readTranscript(root, true)
      assert.deepEqual(tLate.steps.map((s) => s.step_index), [0, 1, 2, 3, 4])
      assert.ok(tLate.revision, 'late arrival must update revision')

      // Appended rewind replacement branch.
      const s1Rewind = { step_index: 1, type: 'PLANNER_RESPONSE', status: 'DONE', created_at: '2026-11-21T00:00:10Z', content: 'rewound reply' }
      appendFileSync(file, line(s1Rewind))
      const tRewind = readTranscript(root, true)
      assert.deepEqual(tRewind.steps.map((s) => s.step_index), [0, 1])
      assert.equal(tRewind.steps[1].content, 'rewound reply')
      assert.ok(tRewind.branch, 'rewind replacement must establish a new branch')
    } finally {
      rmSync(root, { recursive: true, force: true })
    }
  })
})
