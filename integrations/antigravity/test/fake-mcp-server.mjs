// A fake `personad mcp` for the MCP proxy tests: lists two tools and answers a tools/call with the name and arguments
// it received. Usage: node fake-mcp-server.mjs [tools.json]
//   test/change {tools}: lists these from now on and says so (notifications/tools/list_changed), as personad mcp does
//   test/lists: how many tools/list requests it has answered
import { createInterface } from 'node:readline'
import { readFileSync } from 'node:fs'

let tools = process.argv[2] ? JSON.parse(readFileSync(process.argv[2], 'utf8')) : [
  { name: 'todo_create', description: 'Add a to-do', inputSchema: { type: 'object', properties: { title: { type: 'string' } } } },
  { name: 'agents_list', description: 'List the Agents', inputSchema: { type: 'object', properties: {} } },
]
const send = (msg) => process.stdout.write(JSON.stringify(msg) + '\n')
let pendingList
let lists = 0
createInterface({ input: process.stdin }).on('line', (line) => {
  const msg = JSON.parse(line)
  if (msg.method === 'initialize') {
    send({ jsonrpc: '2.0', id: msg.id, result: { protocolVersion: '2025-06-18', instructions: 'Persona tools reach the fixture.' } })
  } else if (msg.method === 'tools/list') {
    lists++
    const response = msg.params?.fail
      ? { error: { code: -32603, message: 'fixture list failure' } }
      : { result: { tools } }
    const reply = { jsonrpc: '2.0', id: msg.id, ...response }
    if (msg.params?.hold) pendingList = reply
    else send(reply)
  }
  else if (msg.method === 'test/echo') send({ jsonrpc: '2.0', id: msg.id, result: msg.params })
  else if (msg.method === 'test/change') {
    tools = msg.params.tools
    send({ jsonrpc: '2.0', id: msg.id, result: {} })
    send({ jsonrpc: '2.0', method: 'notifications/tools/list_changed' })
  } else if (msg.method === 'test/lists') send({ jsonrpc: '2.0', id: msg.id, result: { lists } })
  else if (msg.method === 'tools/call') {
    const { name, arguments: args } = msg.params
    if (!tools.some((t) => t.name === name)) send({ jsonrpc: '2.0', id: msg.id, error: { code: -32602, message: `unknown tool ${name}` } })
    else if (name === 'tool_catalog') send({ jsonrpc: '2.0', id: msg.id, result: { tools } })
    else send({ jsonrpc: '2.0', id: msg.id, result: { content: [{ type: 'text', text: JSON.stringify({ name, args }) }] } })
    if (pendingList) {
      send(pendingList)
      pendingList = undefined
    }
  } else if (msg.id !== undefined) send({ jsonrpc: '2.0', id: msg.id, result: {} })
})
