import { readdirSync, readFileSync } from 'node:fs'
import { join } from 'node:path'
import { CONVERSATION_ID } from './overlay.js'

export function isSwarmWorkerTitle(title) {
  return typeof title === 'string' && Boolean(title.trim().match(/^Swarm Worker \(([\s\S]*)\)$/)?.[1].trim())
}

// Native summaries are authoritative. Recover missing edges from descriptors
// written under the conversation that launched the worker.
export function recoverSubagentParents(brainDirOf, conversations, missing) {
  const parents = new Map()
  if (!missing.size) return parents
  for (const parent of conversations) {
    if (!CONVERSATION_ID.test(parent)) continue
    const dir = join(brainDirOf(parent), '.system_generated', 'subagents')
    let files
    try {
      files = readdirSync(dir, { withFileTypes: true })
    } catch {
      continue
    }
    for (const file of files) {
      if (!file.isFile() || !file.name.endsWith('.json')) continue
      const id = file.name.slice(0, -5)
      if (id === parent || !missing.has(id)) continue
      try {
        const descriptor = JSON.parse(readFileSync(join(dir, file.name), 'utf8'))
        if (descriptor.conversationId === id && descriptor.subagentDescriptor) {
          // Conflicting on-disk claims cannot identify an owner.
          parents.set(id, parents.has(id) ? '' : parent)
        }
      } catch {
        // The native writer may still be writing the descriptor.
      }
    }
  }
  return parents
}

export function rootConversation(parentOf, id) {
  const seen = new Set([id])
  for (let parent; (parent = parentOf(id)) && !seen.has(parent); id = parent) {
    seen.add(parent)
  }
  return id
}
