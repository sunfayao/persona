import assert from 'node:assert/strict'
import { appendFileSync, closeSync, mkdirSync, mkdtempSync, openSync, renameSync, rmSync, writeFileSync, writeSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { test } from 'node:test'
import { readTranscript, readTranscriptSteps } from '../lib/map.js'

function logs(t) {
  const brain = mkdtempSync(join(tmpdir(), 'agy-transcript-'))
  const directory = join(brain, '.system_generated/logs')
  mkdirSync(directory, { recursive: true })
  t.after(() => rmSync(brain, { recursive: true, force: true }))
  return { brain, directory }
}

const record = (content) => JSON.stringify({ step_index: 0, type: 'PLANNER_RESPONSE', content }) + '\n'

test('the complete transcript is authoritative while the compact export is ahead', (t) => {
  const { brain, directory } = logs(t)
  writeFileSync(join(directory, 'transcript.jsonl'), record('compact') + JSON.stringify({ step_index: 1, content: 'next' }) + '\n')
  writeFileSync(join(directory, 'transcript_full.jsonl'), record('complete') + '{"step_index":')
  assert.deepEqual(readTranscriptSteps(brain, true).map((step) => step.content), ['complete'])
  assert.equal(readTranscriptSteps(brain, false).length, 2)
})

test('complete reads fall back to compact-only histories and unavailable full files', (t) => {
  const { brain, directory } = logs(t)
  writeFileSync(join(directory, 'transcript.jsonl'), record('compact-only history'))
  assert.equal(readTranscriptSteps(brain, true)[0].content, 'compact-only history')
  mkdirSync(join(directory, 'transcript_full.jsonl'))
  assert.equal(readTranscriptSteps(brain, true)[0].content, 'compact-only history')
})

test('fresh rewrites bypass the cache and settled records refresh after atomic replacement', async (t) => {
  const { brain, directory } = logs(t)
  const file = join(directory, 'transcript_full.jsonl')
  writeFileSync(file, record('first'))
  assert.equal(readTranscriptSteps(brain, true)[0].content, 'first')
  assert.equal(readTranscriptSteps(brain, true)[0].content, 'first')
  writeFileSync(file, record('other'))
  assert.equal(readTranscriptSteps(brain, true)[0].content, 'other')
  await new Promise((resolve) => setTimeout(resolve, 1100))
  const cached = readTranscriptSteps(brain, true)
  assert.equal(readTranscriptSteps(brain, true), cached, 'settled files reuse parsed records')
  writeFileSync(file + '.new', record('final'))
  renameSync(file + '.new', file)
  assert.equal(readTranscriptSteps(brain, true)[0].content, 'final')
})

test('native replacement records discard the abandoned tail and keep a stable branch identity', (t) => {
  const { brain, directory } = logs(t)
  const file = join(directory, 'transcript_full.jsonl')
  const step = (index, text, at) => JSON.stringify({ step_index: index, type: 'PLANNER_RESPONSE', content: text, created_at: at }) + '\n'
  writeFileSync(file, step(0, 'retained', '2026-01-01') + step(1, 'abandoned', '2026-01-01') + step(2, 'abandoned tail', '2026-01-01'))
  assert.equal(readTranscript(brain, true).branch, '')
  appendFileSync(file, step(1, 'replacement', '2026-01-02'))
  const rewound = readTranscript(brain, true)
  assert.deepEqual(rewound.steps.map((step) => step.content), ['retained', 'replacement'])
  assert.ok(rewound.branch)
  appendFileSync(file, step(1, 'replacement updated', '2026-01-02') + step(2, 'continued', '2026-01-02'))
  const next = readTranscript(brain, true)
  assert.equal(next.branch, rewound.branch, 'updating the same step and continuing keep the branch')
  assert.ok(next.revision, 'a changed native step has a content revision')
  assert.deepEqual(next.steps.map((step) => step.content), ['retained', 'replacement updated', 'continued'])
  appendFileSync(file, step(3, 'ordinary append', '2026-01-02'))
  assert.equal(readTranscript(brain, true).revision, next.revision, 'ordinary appends keep the content revision')
})

test('same-sequence file rewrites update content revisions while ordinary appends keep them', async (t) => {
  const { brain, directory } = logs(t)
  const file = join(directory, 'transcript_full.jsonl')
  writeFileSync(file, record('Pending'))
  const pending = readTranscript(brain, true)
  writeFileSync(file, record('Complete'))
  const complete = readTranscript(brain, true)
  assert.notEqual(complete.revision, pending.revision)
  assert.equal(readTranscript(brain, true).revision, complete.revision)
  for (let i = 0; i < 20; i++) {
    const other = join(directory, 'other-' + i, '.system_generated/logs')
    mkdirSync(other, { recursive: true })
    writeFileSync(join(other, 'transcript_full.jsonl'), record('Other conversation'))
  }
  await new Promise((resolve) => setTimeout(resolve, 1100))
  readTranscript(brain, true)
  for (let i = 0; i < 20; i++) readTranscript(join(directory, 'other-' + i), true)
  assert.equal(readTranscript(brain, true).revision, complete.revision, 'body cache pressure preserves the revision')
  appendFileSync(file, JSON.stringify({ step_index: 1, type: 'PLANNER_RESPONSE', content: 'Next' }) + '\n')
  assert.equal(readTranscript(brain, true).revision, complete.revision)
})

// A parallel call that fails at once (unknown tool, bad arguments) is exported before its sibling's result, so the
// lines arrive as 3 then 2. That is a late arrival, not a rewind: both results belong to the same planner step.
test('parallel results exported out of order are kept in step order without mistaking them for a rewind', (t) => {
  const { brain, directory } = logs(t)
  const file = join(directory, 'transcript.jsonl')
  const line = (step) => JSON.stringify({ source: 'MODEL', status: 'DONE', created_at: '2026-01-01T00:00:00Z', ...step }) + '\n'
  const calls = [{ name: 'run_command', args: { CommandLine: 'tail log' } }, { name: 'manage_task', args: { Action: 'status' } }]
  writeFileSync(file, line({ step_index: 0, type: 'USER_INPUT', source: 'USER_EXPLICIT', content: 'go' }) +
    line({ step_index: 1, type: 'PLANNER_RESPONSE', tool_calls: calls }) +
    line({ step_index: 3, type: 'GENERIC', status: 'INVALID', content: 'Error invalid tool call', error: 'invalid arguments' }))
  const before = readTranscript(brain, false)
  appendFileSync(file, line({ step_index: 2, type: 'GENERIC', content: 'tail output' }) +
    line({ step_index: 4, type: 'PLANNER_RESPONSE', tool_calls: [{ name: 'view_file', args: { AbsolutePath: '/tmp/a' } }] }) +
    line({ step_index: 5, type: 'GENERIC', content: 'file body' }))
  const after = readTranscript(brain, false)
  assert.deepEqual(after.steps.map((step) => step.step_index), [0, 1, 2, 3, 4, 5], 'the late step keeps its place and nothing is dropped')
  assert.equal(after.branch, before.branch, 'a late arrival is not a rewind: item identities stay')
  assert.notEqual(after.revision, before.revision, 'watchers still learn that earlier items changed')

})

test('parsed transcript cache retains a full inventory of 80 sessions', async (t) => {
  const entries = Array.from({ length: 80 }, () => logs(t))
  for (const { directory } of entries) writeFileSync(join(directory, 'transcript_full.jsonl'), record('Cached reply.'))
  await new Promise((resolve) => setTimeout(resolve, 1100))
  const cached = entries.map(({ brain }) => readTranscript(brain, true))
  for (const [index, { brain }] of entries.entries()) assert.equal(readTranscript(brain, true), cached[index])
})

test('incremental append handles large files, partial trailing lines, and preserves prior snapshots', (t) => {
  const { brain, directory } = logs(t)
  const file = join(directory, 'transcript_full.jsonl')
  const pad = 'p'.repeat(600)
  const makeStep = (i, content) => JSON.stringify({
    step_index: i,
    source: i % 2 === 0 ? 'USER_EXPLICIT' : 'MODEL',
    type: i % 2 === 0 ? 'USER_INPUT' : 'PLANNER_RESPONSE',
    status: 'DONE',
    created_at: `2026-11-20T10:00:${String(i % 60).padStart(2, '0')}Z`,
    content,
  }) + '\n'

  let initial = ''
  for (let i = 0; i < 12; i++) initial += makeStep(i, `step-${i}-${pad}`)
  // Trailing partial JSON line without newline
  writeFileSync(file, initial + '{"step_index":12,"type":"PLANNER_')
  const first = readTranscript(brain, true)
  assert.equal(first.steps.length, 12)
  // Unsettled unchanged read reuses cached transcript reference via SHA-256 verification
  assert.equal(readTranscript(brain, true), first)

  // Complete the partial line and append step 13 (without trailing newline)
  const step13NoNl = makeStep(13, 'step-13-no-nl').trimEnd()
  appendFileSync(file, 'RESPONSE","status":"DONE","created_at":"2026-11-20T10:01:00Z","content":"step-12-done"}\n' + step13NoNl)
  const second = readTranscript(brain, true)
  assert.equal(first.steps.length, 12, 'prior snapshot array must not be mutated')
  assert.equal(second.steps.length, 14)
  assert.equal(second.steps[12].content, 'step-12-done')
  assert.equal(second.steps[13].content, 'step-13-no-nl')
  assert.equal(second.revision, first.revision)
  assert.equal(second.steps[0], first.steps[0], 'incremental append reuses parsed prefix step objects')

  // Complete newline for step 13 and append step 14
  appendFileSync(file, '\n' + makeStep(14, 'step-14-done'))
  const third = readTranscript(brain, true)
  assert.equal(third.steps.length, 15)
  assert.equal(third.steps[13].content, 'step-13-no-nl')
  assert.equal(third.steps[14].content, 'step-14-done')
  assert.equal(third.revision, first.revision)
})

test('same-size fast rewrite and truncation are always detected even on large unsettled files', (t) => {
  const { brain, directory } = logs(t)
  const file = join(directory, 'transcript_full.jsonl')
  const pad = 'a'.repeat(500)
  const line = (i, tag) => JSON.stringify({
    step_index: i,
    source: 'MODEL',
    type: 'PLANNER_RESPONSE',
    status: 'DONE',
    created_at: '2026-11-20T10:00:00Z',
    content: `${tag}-${pad}`,
  }) + '\n'

  const rowsA = Array.from({ length: 16 }, (_, i) => line(i, 'ORIG'))
  writeFileSync(file, rowsA.join(''))
  const before = readTranscript(brain, true)
  assert.equal(before.steps[8].content.startsWith('ORIG-'), true)

  // Same-size fast rewrite in the middle of a >8KB file within the same mtime tick
  const rowsB = rowsA.slice()
  rowsB[8] = line(8, 'EDIT')
  assert.equal(Buffer.byteLength(rowsB.join('')), Buffer.byteLength(rowsA.join('')))
  writeFileSync(file, rowsB.join(''))
  const rewritten = readTranscript(brain, true)
  assert.equal(rewritten.steps[8].content.startsWith('EDIT-'), true, 'middle same-size rewrite must not be missed')
  assert.notEqual(rewritten.revision, before.revision)

  // Prefix rewrite + growth (tail changes because middle line grew)
  const rowsC = rowsB.slice()
  rowsC[1] = line(1, 'LONGER_PREFIX_REWRITE')
  rowsC.push(line(16, 'TAIL'))
  writeFileSync(file, rowsC.join(''))
  const grownRewrite = readTranscript(brain, true)
  assert.equal(grownRewrite.steps.length, 17)
  assert.equal(grownRewrite.steps[1].content.startsWith('LONGER_PREFIX_REWRITE-'), true)
  assert.notEqual(grownRewrite.revision, rewritten.revision)

  // Truncation to fewer steps
  writeFileSync(file, rowsA.slice(0, 2).join(''))
  const truncated = readTranscript(brain, true)
  assert.equal(truncated.steps.length, 2)
  assert.notEqual(truncated.revision, grownRewrite.revision)
})

test('incremental append handles rewind replacement and out-of-order late arrival on large files', (t) => {
  const { brain, directory } = logs(t)
  const file = join(directory, 'transcript_full.jsonl')
  const pad = 'z'.repeat(600)
  const line = (step) => JSON.stringify({ source: 'MODEL', status: 'DONE', created_at: '2026-11-20T10:00:00Z', ...step }) + '\n'

  let initial = ''
  for (let i = 0; i < 10; i++) initial += line({ step_index: i, type: 'PLANNER_RESPONSE', content: `base-${i}-${pad}` })
  initial += line({ step_index: 10, type: 'PLANNER_RESPONSE', tool_calls: [{ name: 'run_command', args: { CommandLine: 'a' } }, { name: 'run_command', args: { CommandLine: 'b' } }] })
  initial += line({ step_index: 12, type: 'GENERIC', content: 'second-finished-first' })
  writeFileSync(file, initial)
  const before = readTranscript(brain, true)
  assert.deepEqual(before.steps.slice(-2).map((s) => s.step_index), [10, 12])

  // Out-of-order late arrival of step 11 appended incrementally
  appendFileSync(file, line({ step_index: 11, type: 'GENERIC', content: 'first-arrived-late' }))
  const late = readTranscript(brain, true)
  assert.deepEqual(late.steps.slice(-3).map((s) => s.step_index), [10, 11, 12])
  assert.equal(late.branch, before.branch)
  assert.notEqual(late.revision, before.revision)

  // Rewind replacement appended incrementally
  appendFileSync(file, line({ step_index: 9, type: 'PLANNER_RESPONSE', content: 'rewound-9', created_at: '2026-11-20T10:05:00Z' }))
  const rewound = readTranscript(brain, true)
  assert.equal(rewound.steps.length, 10)
  assert.equal(rewound.steps[9].content, 'rewound-9')
  assert.notEqual(rewound.branch, late.branch)
})

test('same-inode growth detects an in-place same-length prefix edit anywhere in the file before append', (t) => {
  const { brain, directory } = logs(t)
  const file = join(directory, 'transcript_full.jsonl')
  const line = (i, tag) => JSON.stringify({
    step_index: i,
    source: 'MODEL',
    type: 'PLANNER_RESPONSE',
    status: 'DONE',
    created_at: '2026-11-20T10:00:00Z',
    content: `${tag}-${String(i).padStart(3, '0')}`,
  }) + '\n'

  const rows = Array.from({ length: 200 }, (_, i) => line(i, 'ORIG'))
  const initial = rows.join('')
  assert.ok(Buffer.byteLength(initial) > 20 * 1024, 'fixture should exceed 20KB')
  writeFileSync(file, initial)

  const before = readTranscript(brain, true)
  assert.equal(before.steps.length, 200)
  assert.equal(before.steps[60].content, 'ORIG-060')

  // In-place same-length overwrite of step 60 (outside head/mid/tail windows) followed by appending step 200
  const offset60 = Buffer.byteLength(rows.slice(0, 60).join(''))
  const edited60 = Buffer.from(line(60, 'EDIT'))
  assert.equal(edited60.length, Buffer.byteLength(rows[60]))
  const fd = openSync(file, 'r+')
  try {
    writeSync(fd, edited60, 0, edited60.length, offset60)
  } finally {
    closeSync(fd)
  }
  appendFileSync(file, line(200, 'TAIL'))

  const after = readTranscript(brain, true)
  assert.equal(after.steps.length, 201)
  assert.equal(after.steps[60].content, 'EDIT-060', 'in-place edit at step 60 must not be hidden by growth path')
  assert.equal(after.steps[200].content, 'TAIL-200')
  assert.notEqual(after.revision, before.revision)
})
