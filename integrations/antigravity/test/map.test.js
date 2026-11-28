import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { agyModeToPersona, cleanUserText, personaModeToAgy, toAgyId, toPersonaId } from '../lib/map.js'

describe('Antigravity control helpers', () => {
  it('converts session ids between persona and agy', () => {
    assert.equal(toPersonaId('ad40e999-0253-4097-8ee2-a70f9259e88b'), 'agy-ad40e999-0253-4097-8ee2-a70f9259e88b')
    assert.equal(toPersonaId('agy-ad40e999-0253-4097-8ee2-a70f9259e88b'), 'agy-ad40e999-0253-4097-8ee2-a70f9259e88b')
    assert.equal(toAgyId('agy-ad40e999-0253-4097-8ee2-a70f9259e88b'), 'ad40e999-0253-4097-8ee2-a70f9259e88b')
  })

  it('extracts user request and strips persona-context', () => {
    const raw = `<USER_REQUEST>\n<persona-context>\nsnapshot\n</persona-context>\n\nHello world\n</USER_REQUEST>\n<ADDITIONAL_METADATA>\ntime\n</ADDITIONAL_METADATA>`
    assert.equal(cleanUserText(raw), 'Hello world')
  })

  it('matches native context resources without hiding ordinary user labels', () => {
    const user = 'Please reply OK.\nKeep this second line.'
    const label = 'Content of persona-context:///snapshot.txt:\n'
    const context = '<persona-context>Attached context with /compact</persona-context>'
    const cases = [
      ['native first request', `<USER_REQUEST>\n${label}${context}${user}\n</USER_REQUEST>\n<ADDITIONAL_METADATA>clock</ADDITIONAL_METADATA>`, user],
      ['unwrapped resource', label + context + user, user],
      ['another Persona URI', 'Content of persona-context:///other.txt:\r\n' + context + user, user],
      ['successive resources', label + context + '\n' + label + context + user, user],
      ['no prefix', user, user],
      ['literal label', label + user, label + user],
      ['other URI', 'Content of file:///snapshot.txt:\n' + context + user, 'Content of file:///snapshot.txt:\n' + user],
      ['similar scheme', 'Content of persona-context-extra:///snapshot.txt:\n' + context + user, 'Content of persona-context-extra:///snapshot.txt:\n' + user],
      ['quoted label', 'Example:\n' + label + user, 'Example:\n' + label + user],
      ['incomplete envelope', label + '<persona-context>unfinished', label + '<persona-context>unfinished'],
    ]
    for (const [name, content, expected] of cases) {
      assert.equal(cleanUserText(content), expected, name)
    }
  })

  it('maps modes between Persona and Antigravity ACP', () => {
    assert.equal(personaModeToAgy('plan'), 'plan')
    assert.equal(personaModeToAgy('acceptEdits'), 'accept-edits')
    assert.equal(personaModeToAgy('auto'), 'auto')
    assert.equal(agyModeToPersona('auto'), 'auto')
    assert.equal(personaModeToAgy('default'), 'default')
    assert.equal(agyModeToPersona('accept-edits'), 'acceptEdits')
  })
})
