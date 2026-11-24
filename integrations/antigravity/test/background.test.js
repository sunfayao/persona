import assert from 'node:assert/strict'
import { test } from 'node:test'
import { backgroundRow, extractBackground } from '../lib/background.js'

const CONV = '11111111-2222-3333-4444-555555555555'

function bgStep(index, taskNum, startedAt, description) {
  return {
    step_index: index,
    type: 'GENERIC',
    status: 'RUNNING',
    created_at: startedAt,
    content: `Created At: ${startedAt}\nTool is running as a background task with task id: ${CONV}/task-${taskNum}\nTask Description: ${description}`,
  }
}

test('one-shot timers, bundled task notices, MaxIterations crons, and manage_task reconciliation settle cleanly', () => {
  const baseMs = Date.now() - 10 * 60_000
  const t0 = new Date(baseMs).toISOString()
  const t1 = new Date(baseMs + 30_000).toISOString()
  const t2 = new Date(baseMs + 60_000).toISOString()
  const t3 = new Date(baseMs + 90_000).toISOString()

  const steps = [
    {
      step_index: 0,
      type: 'PLANNER_RESPONSE',
      created_at: t0,
      tool_calls: [
        { name: 'run_command', args: { CommandLine: '"sleep 5"' } },
        { name: 'schedule', args: { DurationSeconds: '240', Prompt: '"Check task-1"', TimerCondition: `"${CONV}/task-1"` } },
        { name: 'schedule', args: { DurationSeconds: 30, Prompt: '"Wake up"' } },
        { name: 'schedule', args: { DurationSeconds: '6000', Prompt: '"Long timer"' } },
        { name: 'schedule', args: { CronExpression: '"*/2 * * * *"', MaxIterations: '2', Prompt: '"Poll MR"' } },
        { name: 'run_command', args: { CommandLine: '"cmd6"' } },
        { name: 'run_command', args: { CommandLine: '"cmd7"' } },
        { name: 'run_command', args: { CommandLine: '"cmd8"' } },
      ],
    },
    bgStep(1, 1, t0, 'sleep 5'),
    bgStep(2, 2, t0, 'Timer: 4m, Prompt: Check task-1'),
    bgStep(3, 3, t0, 'Timer: 30s, Prompt: Wake up'),
    bgStep(4, 4, t0, 'Timer: 1h 40m, Prompt: Long timer'),
    bgStep(5, 5, t0, 'Cron: */2 * * * *, Prompt: Poll MR'),
    bgStep(6, 6, t0, 'cmd6'),
    bgStep(7, 7, t0, 'cmd7'),
    bgStep(8, 8, t0, 'cmd8'),
    {
      step_index: 9,
      type: 'SYSTEM_MESSAGE',
      created_at: t1,
      content: `<SYSTEM_MESSAGE>\n[Message] timestamp=${t1} sender=${CONV}/task-1 content=Task id "${CONV}/task-1" finished with result: ok\n</SYSTEM_MESSAGE>\n<SYSTEM_MESSAGE>\n[Message] timestamp=${t1} sender=${CONV}/task-3 content=Wake up\n</SYSTEM_MESSAGE>\n<SYSTEM_MESSAGE>\n[Message] timestamp=${t1} sender=${CONV}/task-5 content=Poll MR\n</SYSTEM_MESSAGE>`,
    },
    {
      step_index: 10,
      type: 'SYSTEM_MESSAGE',
      created_at: t2,
      content: `<SYSTEM_MESSAGE>\n[Message] timestamp=${t2} sender=${CONV}/task-6 content=Task id "${CONV}/task-6" finished\n</SYSTEM_MESSAGE>\n<SYSTEM_MESSAGE>\n[Message] timestamp=${t2} sender=${CONV}/task-7 content=Task id "${CONV}/task-7" was canceled\n</SYSTEM_MESSAGE>\n<SYSTEM_MESSAGE>\n[Message] timestamp=${t2} sender=${CONV}/task-5 content=Poll MR\n</SYSTEM_MESSAGE>`,
    },
  ]

  const parsed = extractBackground(steps, CONV)
  assert.deepEqual(parsed.running.map((r) => r.id), [`${CONV}/task-4`, `${CONV}/task-8`])
  assert.equal(JSON.stringify(parsed.running[0]).includes('backgroundDue'), false, 'due symbol stays off JSON wire')

  // Before task-4 dueAt: both task-4 and task-8 are running.
  const beforeDue = backgroundRow(parsed, true, baseMs + 5 * 60_000)
  assert.deepEqual(beforeDue.background.map((r) => r.id), [`${CONV}/task-4`, `${CONV}/task-8`])

  // After task-4 dueAt (without re-running extractBackground!): task-4 settles to completed.
  const afterDue = backgroundRow(parsed, true, baseMs + 2 * 3600_000)
  assert.deepEqual(afterDue.background.map((r) => r.id), [`${CONV}/task-8`])
  assert.equal(afterDue.background_ended.some((r) => r.id === `${CONV}/task-4` && r.status === 'completed'), true)

  // manage_task list reporting no running tasks reconciles the rest.
  const afterList = extractBackground([
    ...steps,
    { step_index: 11, type: 'GENERIC', status: 'DONE', created_at: t3, content: `Created At: ${t3}\nCompleted At: ${t3}\nNo background tasks are currently running.` },
  ], CONV)
  assert.deepEqual(afterList.running, [])
})
