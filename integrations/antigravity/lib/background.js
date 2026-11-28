const startPrefix = 'Tool is running as a background task with task id:'
const endNotice = /\bTask id "([^"\n]+)" (?:was )?(finished|failed|canceled|cancelled|killed|stopped)\b/gi
const serverRestartNotice = /\bAll your subagents and background tasks have been stopped due to server restart\b/i
const msgHeader = /\[Message\]([^\n]*)/g
const msgSender = /\bsender=([^\s]+)/
const msgTimestamp = /\btimestamp=([^\s]+)/
const taskCancelled = /\bTask "([^"\n]+)" cancelled\./g
const taskStatusLine = /^Task:\s*(\S+)\s*\nStatus:\s*(\w+)\b/gm
const terminalStatuses = new Map([
  ['DONE', 'completed'], ['COMPLETED', 'completed'],
  ['ERROR', 'failed'], ['FAILED', 'failed'],
  ['CANCELED', 'killed'], ['CANCELLED', 'killed'], ['KILLED', 'killed'], ['STOPPED', 'killed'],
])
const kinds = new Map([['schedule', 'monitor'], ['run_workflow', 'workflow'], ['invoke_subagent', 'agent']])
const retainedForMs = 24 * 60 * 60 * 1000
const endedLimit = 8
const runningLimit = 50
const taskName = /^task-[0-9]+$/
const liveStep = new Set(['RUNNING', 'PENDING', 'IN_PROGRESS', 'QUEUED'])
const dueSymbol = Symbol('backgroundDue')

// Step parsing can replace a GENERIC result with its final output. Keep the
// original task notice as private metadata so the outcome still has an ID.
export const backgroundStart = Symbol('backgroundStart')

export function backgroundNotice(content) {
  if (typeof content !== 'string') return ''
  content = content.trim()
  if (content.startsWith('Created At:')) {
    const newline = content.indexOf('\n')
    content = newline < 0 ? '' : content.slice(newline + 1).trimStart()
  }
  return content.startsWith(startPrefix) ? content.slice(startPrefix.length) : ''
}

export function priorBackgroundNotice(step) {
  if (step[backgroundStart]) return step[backgroundStart]
  if (liveStep.has(String(step.status).trim().toUpperCase()) && backgroundNotice(step.content)) return step.content
  return ''
}

function toolString(value) {
  if (typeof value !== 'string') return ''
  if (value.startsWith('"')) {
    try { return JSON.parse(value) } catch { /* The argument was already decoded. */ }
  }
  return value
}

function toolInt(value) {
  if (typeof value === 'number' && Number.isFinite(value)) return Math.trunc(value)
  const text = toolString(value).trim()
  return /^-?\d+$/.test(text) ? Number.parseInt(text, 10) : 0
}

function parseTimerDescMs(description) {
  if (typeof description !== 'string') return 0
  const m = /^Timer:\s*([^,]+)/.exec(description.trim())
  if (!m) return 0
  let total = 0, matched = false
  for (const part of m[1].matchAll(/(\d+)\s*([hms])/g)) {
    matched = true
    const n = Number.parseInt(part[1], 10)
    total += part[2] === 'h' ? n * 3600_000 : part[2] === 'm' ? n * 60_000 : n * 1000
  }
  return matched ? total : 0
}

function finish(record, status, at, inferred = false) {
  if (!record || (record.task.status && (!record.inferred || inferred))) return
  record.task.status = status
  record.task.ended_at = at
  record.inferred = inferred
}

function reconcileManageTask(content, at, records) {
  if (typeof content !== 'string' || !content) return
  for (const m of content.matchAll(taskCancelled)) {
    finish(records.get(m[1]), 'killed', at)
  }
  for (const m of content.matchAll(taskStatusLine)) {
    const status = terminalStatuses.get(m[2].toUpperCase())
    if (status) finish(records.get(m[1]), status, at)
  }
  if (content.includes('No background tasks are currently running.')) {
    for (const rec of records.values()) {
      if (!rec.task.status) finish(rec, 'completed', at, true)
    }
    return
  }
  const marker = ' background task(s) currently running:\n['
  const idx = content.indexOf(marker)
  if (idx >= 0) {
    let raw = content.slice(idx + marker.length - 1)
    const nl = raw.indexOf('\n')
    if (nl >= 0) raw = raw.slice(0, nl)
    try {
      const parsed = JSON.parse(raw.trim())
      if (Array.isArray(parsed)) {
        const live = new Set(parsed.map(entry => entry?.taskId).filter(Boolean))
        for (const rec of records.values()) {
          if (!rec.task.status && !live.has(rec.task.id)) finish(rec, 'completed', at, true)
        }
      }
    } catch { /* Ignore truncated JSON output. */ }
  }
}

export function extractBackground(steps, conversation) {
  const records = new Map()
  const prefix = conversation + '/'
  let planner, plannerIndex
  let index = -1
  let lastStepMs = 0
  for (const step of steps) {
    index = Number.isInteger(step?.step_index) && step.step_index >= 0 ? step.step_index : index + 1
    const stepAt = step?.updated_at || step?.created_at || ''
    const stepMs = Date.parse(stepAt)
    if (Number.isFinite(stepMs) && stepMs > lastStepMs) lastStepMs = stepMs
    switch (step?.type) {
      case 'PLANNER_RESPONSE':
        planner = Array.isArray(step.tool_calls) ? step.tool_calls : undefined
        plannerIndex = index
        for (const call of planner ?? []) {
          if (call?.name === 'manage_task' && toolString(call.args?.Action).toLowerCase() === 'kill') {
            finish(records.get(toolString(call.args?.TaskId)), 'killed', step.created_at)
          }
        }
        break
      case 'USER_INPUT':
        planner = undefined
        break
      case 'SYSTEM_MESSAGE': {
        const text = step.content ?? ''
        for (const notice of text.matchAll(endNotice)) {
          const verb = notice[2].toLowerCase()
          const status = verb === 'finished' ? 'completed' : verb === 'failed' ? 'failed' : 'killed'
          finish(records.get(notice[1]), status, step.created_at)
        }
        let restartHandled = false
        for (const m of text.matchAll(msgHeader)) {
          const hdr = m[1]
          const tm = msgTimestamp.exec(hdr)
          const msgAt = tm && Number.isFinite(Date.parse(tm[1])) ? tm[1] : step.created_at
          if (serverRestartNotice.test(hdr)) {
            restartHandled = true
            for (const rec of records.values()) if (!rec.task.status) finish(rec, 'killed', msgAt)
          }
          const sm = msgSender.exec(hdr)
          if (!sm) continue
          const sender = sm[1]
          const own = records.get(sender)
          if (own) {
            if (own.oneShot) {
              finish(own, 'completed', msgAt)
            } else if (own.maxIterations > 0) {
              own.triggers++
              if (own.triggers >= own.maxIterations) finish(own, 'completed', msgAt)
            }
          }
          for (const rec of records.values()) {
            if (rec.task.status || !rec.oneShot || !rec.condition || rec.condition.toLowerCase() === 'never' || sender === rec.task.id) continue
            if (rec.condition.toLowerCase() === 'any' || sender === rec.condition || sender === prefix + rec.condition) {
              finish(rec, 'killed', msgAt)
            }
          }
        }
        if (!restartHandled && serverRestartNotice.test(text)) {
          for (const rec of records.values()) if (!rec.task.status) finish(rec, 'killed', step.created_at)
        }
        break
      }
      case 'GENERIC': {
        const notice = backgroundNotice(step[backgroundStart] || step.content)
        if (!notice) {
          if (String(step.status).toUpperCase() === 'DONE') {
            reconcileManageTask(step.content, step.updated_at || step.created_at, records)
          }
          break
        }
        const [first, ...details] = notice.split('\n')
        const id = first.trim()
        if (!id.startsWith(prefix) || !taskName.test(id.slice(prefix.length))) break
        let rec = records.get(id)
        if (!rec) {
          const call = planner?.[index - plannerIndex - 1]
          const description = details.map(line => line.trim()).find(line => line.startsWith('Task Description:'))
          const task = { id, kind: kinds.get(call?.name) ?? 'shell', started_at: step.created_at }
          if (description) task.description = description.slice('Task Description:'.length).trim()
          if (call?.name === 'run_command') task.command = toolString(call.args?.CommandLine) || toolString(call.args?.command)
          rec = { task, inferred: false, oneShot: false, dueMs: 0, condition: '', maxIterations: 0, triggers: 0 }
          const startedMs = Date.parse(step.created_at)
          if (call?.name === 'schedule') {
            const secs = toolInt(call.args?.DurationSeconds)
            const cron = toolString(call.args?.CronExpression).trim()
            const cond = toolString(call.args?.TimerCondition).trim()
            const maxIter = toolInt(call.args?.MaxIterations)
            if (secs > 0) {
              rec.oneShot = true
              rec.condition = cond
              if (Number.isFinite(startedMs)) rec.dueMs = startedMs + secs * 1000
            } else if (!cron) {
              const descMs = parseTimerDescMs(task.description)
              if (descMs > 0) {
                rec.oneShot = true
                rec.condition = cond
                if (Number.isFinite(startedMs)) rec.dueMs = startedMs + descMs
              }
            } else if (maxIter > 0) {
              rec.maxIterations = maxIter
            }
          } else {
            const descMs = parseTimerDescMs(task.description)
            if (descMs > 0) {
              task.kind = 'monitor'
              rec.oneShot = true
              if (Number.isFinite(startedMs)) rec.dueMs = startedMs + descMs
            }
          }
          records.set(id, rec)
        }
        const status = terminalStatuses.get(String(step.status).toUpperCase())
        if (status) finish(rec, status, step.updated_at || step.created_at, true)
        break
      }
    }
  }
  const running = [], ended = []
  for (const rec of records.values()) {
    if (!rec.task.status && rec.dueMs > 0 && lastStepMs >= rec.dueMs) {
      finish(rec, 'completed', new Date(rec.dueMs).toISOString(), true)
    }
    if (rec.task.status) {
      if (Number.isFinite(Date.parse(rec.task.ended_at))) ended.push({ ...rec.task })
    } else if (running.length < runningLimit) {
      const copy = { ...rec.task }
      if (rec.dueMs > 0) copy[dueSymbol] = rec.dueMs
      running.push(copy)
    }
  }
  ended.sort((a, b) => Date.parse(a.ended_at) - Date.parse(b.ended_at))
  return { running, ended: ended.slice(-endedLimit) }
}

// The node attaches log paths from its own local transcript after validating
// that each path belongs to the conversation and task.
export function backgroundRow(tasks, live, now = Date.now()) {
  const running = []
  const extraEnded = []
  for (const task of tasks.running) {
    const dueMs = task[dueSymbol]
    if (dueMs > 0 && now >= dueMs) {
      extraEnded.push({ ...task, status: 'completed', ended_at: new Date(dueMs).toISOString() })
    } else {
      running.push(task)
    }
  }
  let ended = extraEnded.length ? [...tasks.ended, ...extraEnded] : tasks.ended
  if (extraEnded.length) {
    ended.sort((a, b) => Date.parse(a.ended_at) - Date.parse(b.ended_at))
    ended = ended.slice(-endedLimit)
  }
  ended = ended.filter(task => now - Date.parse(task.ended_at) < retainedForMs)
  return {
    ...(live && running.length ? { background: running } : {}),
    ...(ended.length ? { background_ended: ended } : {}),
  }
}
