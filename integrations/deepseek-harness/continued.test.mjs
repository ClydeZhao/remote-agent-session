import { test } from 'node:test'
import assert from 'node:assert/strict'
import { continuedQuestions } from './continued.mjs'

function fixture() {
  const events = [
    { type: 'turn/start', data: { turn: 3 } },
    { type: 'user/message', data: { source: { kind: 'user', rpcId: 'remote' } } },
    { type: 'tool/call', data: { name: 'ask_user_question', callId: 'question' } },
    { type: 'tool/result', data: {} },
    { type: 'turn/end', data: { turn: 3, reason: { kind: 'completed' } } },
  ]
  const agent = { session: { snapshotEvents: () => events } }
  const service = { continued: () => [{ callId: 'question', questions: [{ id: 'q', question: 'Choose' }] }] }
  return { events, agent, service, origins: new Set(['remote']) }
}

test('native continued question retains original source after turn completes', () => {
  const f = fixture()
  const result = continuedQuestions(f.agent, f.service, f.origins)
  assert.equal(result.length, 1)
  assert.equal(result[0].turn_id, '3')
  assert.deepEqual(result[0].origin_ids, ['remote'])
})

for (const kind of ['user', 'unknown-plugin', 'user-question-reply']) {
  test(`later ${kind} input invalidates continued remote question`, () => {
    const f = fixture()
    f.events.push({ type: 'user/message', data: { source: { kind, rpcId: 'new-input' } } })
    assert.deepEqual(continuedQuestions(f.agent, f.service, f.origins), [])
  })
}

test('native question settlement and foreign original input are not revived', () => {
  const f = fixture()
  f.service.continued = () => []
  assert.deepEqual(continuedQuestions(f.agent, f.service, f.origins), [])
  const second = fixture()
  second.events[1].data.source.rpcId = 'local'
  assert.deepEqual(continuedQuestions(second.agent, second.service, second.origins), [])
})

test('internal time context does not expire the original remote question', () => {
  const f = fixture()
  f.events.push({ type: 'user/message', data: { source: { kind: 'time-context' } } })
  assert.equal(continuedQuestions(f.agent, f.service, f.origins).length, 1)
})

test('verified answer to another question preserves pending question with all origins', () => {
  const f = fixture()
  f.agent.id = 'session'
  const reply = { id: 'native-answer', source: { kind: 'user-question-reply', callId: 'other-question' } }
  f.events.push({ type: 'user/message', data: reply })
  const resolve = (session, message) => session === 'session' && message.id === 'native-answer' ? 'remote-answer' : null
  assert.deepEqual(continuedQuestions(f.agent, f.service, f.origins, resolve), [])
  f.origins.add('remote-answer')
  const result = continuedQuestions(f.agent, f.service, f.origins, resolve)
  assert.equal(result.length, 1)
  assert.deepEqual(result[0].origin_ids, ['remote'])
  assert.deepEqual(result[0].all_origin_ids, ['remote', 'remote-answer'])
  f.events.push({ type: 'user/message', data: { source: { kind: 'user', rpcId: 'local' } } })
  assert.deepEqual(continuedQuestions(f.agent, f.service, f.origins, resolve), [])
})
