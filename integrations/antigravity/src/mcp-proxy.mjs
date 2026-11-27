#!/usr/bin/env node
// The readable source of ../mcp-proxy.mjs, the file that runs and is embedded: edit this one, then regenerate that
// one from the repository root with esbuild 0.25.12 (web/node_modules/.bin/esbuild after npm ci in web/), which
// minifies whitespace and syntax only, as #12233 did:
//   esbuild integrations/antigravity/src/mcp-proxy.mjs --minify-whitespace --minify-syntax --format=esm \
//     --platform=node > integrations/antigravity/mcp-proxy.mjs
// scripts/test/test_antigravity_mcp_proxy.py checks that the two agree. Imports are written from ../, where it runs.
//
// Usage: node mcp-proxy.mjs <command> [args...]
//
// Stdio MCP proxy for an assistant chat's `personad mcp` (lib/compat.js mcpServers). agy defers the MCP tools over its
// declaration budget, largest schemas first, and for servers passed through ACP it never declares `call_mcp_tool`, so
// a deferred Persona tool cannot be called at all (#7333). The tools the main conversation calls most are among the
// largest, so agy deferred exactly those. This adds one small tool, persona_call, that calls any Persona tool by name,
// lists them all, and gives one's whole description and arguments (describe); and it gives agy no more than fits its
// budget, so that agy defers nothing: the tools personad mcp marks core (_meta persona/core, most called first by
// persona/rank), then, with assistant.load_all_tools on, the ones it marks persona/fill. The rest stay callable through
// persona_call. A server that marks none (an older personad mcp) gets every tool larger than persona_call, as before.
// Either way persona_call is the smallest tool declared, so it is never the one deferred, however many small tools
// connectors add (#10756). Tool declarations also omit enums agy cannot represent.
// shortcut: works around agy 1.5.8; drop it once agy declares call_mcp_tool for ACP servers.
import { spawn } from 'node:child_process'
import { createInterface } from 'node:readline'
import { agyInputSchema } from './lib/mcp-schema.js'

const DISPATCH = 'persona_call'
// Small, so that a tool is seldom too small to be declared beside it; INSTRUCTIONS and the listing say the rest.
const DISPATCH_TOOL = {
  name: DISPATCH,
  description: 'Call a Persona tool by name; no name lists them.',
  inputSchema: {
    type: 'object',
    properties: { name: { type: 'string' }, arguments: { type: 'object', additionalProperties: true }, describe: { type: 'boolean' } },
  },
}
const DESCRIBE = `${DISPATCH} {name, describe: true} returns a tool's whole description and arguments.`
// Added to the server's instructions, which agy keeps beside the tools.
const INSTRUCTIONS = `Call a Persona tool that is not in your tool list with ${DISPATCH} {name, arguments}; ${DISPATCH} ` +
  `with no arguments lists every Persona tool, and ${DESCRIBE}`
// What agy declares of a tool (its name, description and parameters), in bytes: what its largest-first deferral weighs.
const size = (tool) => Buffer.byteLength(JSON.stringify({ name: tool.name, description: tool.description, inputSchema: tool.inputSchema }))
const DISPATCH_SIZE = size(DISPATCH_TOOL)
// How much of that agy declares before it defers. agy is closed source: on 2026-11-27 an assistant chat had the tools
// whose sizes, smallest first, add up to 48.2 KB, and not the next one, at 53.6 KB. Declaring at most this leaves it
// nothing to defer, with a margin for what it counts besides.
const AGY_DECLARE_BUDGET = 44_000
// A tool that does not fit whole is declared with its first paragraph, cut at a sentence's end within this many
// characters, and where the rest is.
const COMPACT_MAX = 1200
// The no-name listing gives each tool's first sentence, cut at a word within this many characters.
const SENTENCE_MAX = 160

const [command, ...args] = process.argv.slice(2)
const child = spawn(command, args, { stdio: ['pipe', 'pipe', 'inherit'] })
let closing = false
let exitCode = 0
let killTimer
function shutdown(signal = 'SIGTERM', code = 0) {
  if (closing) return
  closing = true
  exitCode = code
  process.stdin.pause()
  child.stdin.end()
  child.kill(signal)
  killTimer = setTimeout(() => child.kill('SIGKILL'), 3000)
  killTimer.unref()
}
child.on('error', (err) => {
  process.stderr.write(`persona mcp proxy: ${err.message}\n`)
  if (!child.pid) process.exit(1)
  shutdown('SIGTERM', 1)
})
child.on('exit', (code, signal) => {
  clearTimeout(killTimer)
  process.exit(closing ? exitCode : code ?? (signal ? 1 : 0))
})
for (const signal of ['SIGTERM', 'SIGINT']) process.on(signal, () => shutdown(signal))
process.stdin.on('end', () => shutdown())
process.stdin.on('close', () => shutdown())
process.stdin.on('error', () => shutdown('SIGTERM', 1))
child.stdin.on('error', () => shutdown('SIGTERM', 1))
process.stdout.on('error', () => shutdown('SIGTERM', 1))

let known = new Map() // the server's tools by name, as it gave them in its last tools/list
let declared = new Set() // the names of those agy was given in the last list it asked for
const pending = new Map() // agy's tools/list and initialize requests by id, until the server answers them: their method
const own = new Map() // the proxy's own tools/list requests by id: what to do once the server answers
let ownCount = 0
const send = (msg) => process.stdout.write(JSON.stringify(msg) + '\n')

createInterface({ input: process.stdin, crlfDelay: Infinity }).on('line', (line) => {
  if (closing) return
  const msg = parse(line)
  if (msg?.method && Object.hasOwn(msg, 'id')) {
    if (msg.method === 'tools/list' || msg.method === 'initialize') pending.set(msg.id, msg.method)
    else pending.delete(msg.id)
  }
  if (msg?.method === 'tools/call' && msg.params?.name === DISPATCH) {
    const { name, arguments: input, describe } = msg.params.arguments || {}
    if (!name || describe) { // answered here; the server is asked for its tools first when it has not listed this one
      const answer = () => send({ jsonrpc: '2.0', id: msg.id, result: name ? describeTool(name) : listing() })
      if (name ? known.has(name) : known.size) answer()
      else refresh(answer)
      return
    }
    line = JSON.stringify({ ...msg, params: { ...msg.params, name, arguments: input || {} } }) // the server rejects a name it does not have
  }
  child.stdin.write(line + '\n')
})

createInterface({ input: child.stdout, crlfDelay: Infinity }).on('line', (line) => {
  if (closing) return
  const msg = parse(line)
  if (msg && !Object.hasOwn(msg, 'method') && Object.hasOwn(msg, 'id') && (Object.hasOwn(msg, 'result') || Object.hasOwn(msg, 'error'))) {
    const then = own.get(msg.id)
    if (then) { // the answer to the proxy's own tools/list, which agy did not ask for
      own.delete(msg.id)
      remember(msg.result?.tools)
      then()
      return
    }
    const method = pending.get(msg.id)
    if (pending.delete(msg.id) && msg.result) {
      if (method === 'initialize') {
        line = JSON.stringify({ ...msg, result: { ...msg.result, instructions: [msg.result.instructions, INSTRUCTIONS].filter(Boolean).join(' ') } })
      } else if (Array.isArray(msg.result.tools)) {
        remember(msg.result.tools)
        const declarations = declare(msg.result.tools.map((tool) => ({ ...tool, inputSchema: agyInputSchema(tool.inputSchema) })))
        declared = new Set(declarations.map((tool) => tool.name))
        line = JSON.stringify({ ...msg, result: { ...msg.result, tools: [...declarations, DISPATCH_TOOL] } })
      }
    }
  } else if (msg?.method === 'notifications/tools/list_changed') {
    // The server's tools changed (an extension's came or went, a setting moved): persona_call knows the new ones
    // whether agy lists them again or not, and agy hears of it once it does.
    refresh(() => process.stdout.write(line + '\n'))
    return
  }
  process.stdout.write(line + '\n')
})

// Asks the server for its tools on the proxy's own account, under an id agy does not use; then() once it has answered.
function refresh(then) {
  const id = `persona-proxy-${++ownCount}`
  own.set(id, then)
  child.stdin.write(JSON.stringify({ jsonrpc: '2.0', id, method: 'tools/list' }) + '\n')
}

function remember(tools) {
  if (Array.isArray(tools)) known = new Map(tools.map((tool) => [tool.name, tool]))
}

// The tools agy is given besides persona_call, each larger than it (#10756). Without the server's marks, all of those.
// With them, the core tools by rank, then the fill ones in the server's order, each whole while it fits what is left of
// AGY_DECLARE_BUDGET, else compacted (which also puts one no larger than persona_call above it), else left to
// persona_call, as every other tool is.
function declare(tools) {
  const mark = (tool, key) => tool._meta?.[key]
  if (!tools.some((tool) => mark(tool, 'persona/core') || mark(tool, 'persona/fill'))) return tools.filter((tool) => size(tool) > DISPATCH_SIZE)
  const rank = (tool) => mark(tool, 'persona/rank') ?? Infinity
  const core = tools.filter((tool) => mark(tool, 'persona/core')).sort((a, b) => rank(a) - rank(b))
  const fill = tools.filter((tool) => !mark(tool, 'persona/core') && mark(tool, 'persona/fill'))
  let room = AGY_DECLARE_BUDGET - DISPATCH_SIZE
  const out = []
  for (const tool of [...core, ...fill]) {
    const fit = [tool, compact(tool)].find((t) => size(t) > DISPATCH_SIZE && size(t) <= room)
    if (fit) {
      room -= size(fit)
      out.push(fit)
    }
  }
  return out
}

// A tool's declaration with only its description's first paragraph, cut to COMPACT_MAX, and where the rest is; its
// arguments stay whole.
function compact(tool) {
  let text = String(tool.description || '').split('\n\n')[0]
  if (text.length > COMPACT_MAX) text = text.slice(0, text.lastIndexOf('. ', COMPACT_MAX) + 1) || text.slice(0, COMPACT_MAX)
  return { ...tool, description: `${text} Full rules: ${DISPATCH} {name: "${tool.name}", describe: true}.` }
}

// persona_call {name, describe: true}: the tool as the server gave it, all its rules and its whole input schema.
function describeTool(name) {
  const tool = known.get(name)
  const text = tool ? JSON.stringify({ name, description: tool.description, inputSchema: tool.inputSchema })
    : `No Persona tool is named ${name}; ${DISPATCH} with no name lists them.`
  return { content: [{ type: 'text', text }], isError: !tool }
}

// persona_call with no name: one line for each Persona tool, those in agy's list first, then the rest.
function listing() {
  const line = ({ name, description, inputSchema }) => {
    // The first sentence ends at a full stop after a word or a bracket, so "e.g." does not end it.
    let sentence = String(description || '').split('\n')[0].split(/(?<=(?:[^\s.]{2}|[)\]`'"])[.!?])\s/)[0].replace(/[.!?]$/, '')
    if (sentence.length > SENTENCE_MAX) sentence = sentence.slice(0, sentence.lastIndexOf(' ', SENTENCE_MAX)) + '…'
    else if (sentence) sentence += '.'
    const required = Array.isArray(inputSchema?.required) && inputSchema.required.length ? ` Required: ${inputSchema.required.join(', ')}` : ''
    return `- ${name}:${sentence && ' ' + sentence}${required}`
  }
  const tools = [...known.values()]
  const group = (title, list) => list.length ? `${title}\n${list.map(line).join('\n')}\n\n` : ''
  const text = group('In your tool list:', tools.filter((tool) => declared.has(tool.name))) +
    group(`Call through ${DISPATCH}:`, tools.filter((tool) => !declared.has(tool.name))) + DESCRIBE
  return { content: [{ type: 'text', text }] }
}

function parse(line) {
  try {
    return JSON.parse(line)
  } catch {
    return null
  }
}
