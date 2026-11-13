import { test } from 'node:test'
import assert from 'node:assert/strict'
import { createInteractions } from './interactions.mjs'

function fixture() {
  let clock = 0
  const listeners = new Map()
  const events = [
    { type: 'turn/start', data: { turn: 1 } },
    { type: 'user/message', data: { source: { kind: 'user', rpcId: 'remote' } } },
    { type: 'tool/call', data: { callId: 'call', toolName: 'bash', arguments: { command: 'safe-test' } } },
  ]
  const agent = { session: { snapshotEvents: () => events }, ctx: { on(event, listener) { listeners.set(event, listener); return () => listeners.delete(event) } } }
  const bridge = createInteractions({ now: () => clock, isLive: () => true })
  bridge.bind(agent, { generation: 1, origins: ['remote'], lease_ms: 10000 })
  return { bridge, agent, events, listeners, expire() { clock = 20000 }, request(signal) { return listeners.get('approval/request')({ agent, callId: 'call', toolName: 'bash', signal }, () => 'local') } }
}

test('remote permission returns the exact native single-use vocabulary', async () => {
  const f = fixture()
  try {
    const waiting = f.request()
    const [request] = f.bridge.list(f.agent, 1)
    f.bridge.answer(f.agent, 1, request.id, 'allow')
    assert.equal(await waiting, 'allowed-once')
    assert.throws(() => f.bridge.answer(f.agent, 1, request.id, 'allow'), /inactive/)
  } finally { f.bridge.dispose() }
})

test('local input is delegated and never converted into remote permission', () => {
  const f = fixture()
  f.events[1].data.source.rpcId = 'local'
  assert.equal(f.request(), 'local')
  assert.deepEqual(f.bridge.list(f.agent, 1), [])
  f.bridge.dispose()
})

for (const failure of ['mixed', 'expired', 'cancelled', 'detached']) {
  test(`pending approval cannot survive ${failure}`, async () => {
    const f = fixture()
    const controller = new AbortController()
    const waiting = f.request(controller.signal)
    const [request] = f.bridge.list(f.agent, 1)
    if (failure === 'mixed') f.events.push({ type: 'user/message', data: { source: { kind: 'user', rpcId: 'local' } } })
    if (failure === 'expired') f.expire()
    if (failure === 'cancelled') controller.abort()
    if (failure === 'detached') f.bridge.withdraw(f.agent)
    assert.throws(() => f.bridge.answer(f.agent, 1, request.id, 'allow'), /inactive/)
    assert.equal(await waiting, 'rejected')
    f.bridge.dispose()
  })
}

test('question answers must match original IDs and options', async () => {
  const f = fixture()
  const waiting = f.listeners.get('user-questions/request')({ agent: f.agent, questions: [{ id: 'choice', question: 'Pick', options: [{ label: 'A' }] }] }, () => 'local')
  const [request] = f.bridge.list(f.agent, 1)
  assert.throws(() => f.bridge.answer(f.agent, 1, request.id, { answers: [{ id: 'other', selected: ['A'] }] }), /invalid_answer/)
  const answer = { answers: [{ id: 'choice', selected: [], custom: 'My answer' }] }
  f.bridge.answer(f.agent, 1, request.id, answer)
  assert.deepEqual(await waiting, answer)
  f.bridge.dispose()
})

test('native blocking wait with callId is not mislabeled as timed', async () => {
  const f = fixture()
  const waiting = f.listeners.get('user-questions/request')({ agent: f.agent, wait: { callId: 'call' }, questions: [{ id: 'q', question: 'Choose', options: [{ label: 'yes' }] }] }, () => 'local')
  const [request] = f.bridge.list(f.agent, 1)
  assert.equal(request.timed, false)
  f.bridge.answer(f.agent, 1, request.id, { answers: [{ id: 'q', selected: ['yes'] }] })
  await waiting
  f.bridge.dispose()
})

test('old detach cannot remove a newer interaction binding', async () => {
  const f = fixture()
  f.bridge.bind(f.agent, { generation: 2, origins: ['remote'], lease_ms: 10000 })
  assert.throws(() => f.bridge.withdraw(f.agent, 1), /stale_binding/)
  const waiting = f.request()
  const [request] = f.bridge.list(f.agent, 2)
  f.bridge.answer(f.agent, 2, request.id, 'deny')
  assert.equal(await waiting, 'rejected')
  f.bridge.dispose()
})

test('older heartbeat cannot forget an already admitted remote input', async () => {
  const f = fixture()
  f.bridge.bind(f.agent, { generation: 1, origins: ['remote', 'new-input'], lease_ms: 10000 })
  f.bridge.bind(f.agent, { generation: 1, origins: ['remote'], lease_ms: 10000 })
  f.events[1].data.source.rpcId = 'new-input'
  const waiting = f.request()
  const [request] = f.bridge.list(f.agent, 1)
  assert.deepEqual(request.origin_ids, ['new-input'])
  f.bridge.answer(f.agent, 1, request.id, 'deny')
  assert.equal(await waiting, 'rejected')
  f.bridge.dispose()
})

test('continued discovery does not grant answer authority before origin approval', () => {
  const events = [
    { type: 'turn/start', data: { turn: 1 } },
    { type: 'user/message', data: { source: { kind: 'user', rpcId: 'remote' } } },
    { type: 'tool/call', data: { name: 'ask_user_question', callId: 'call' } },
    { type: 'turn/end', data: { turn: 1 } },
  ]
  const agent = { id: 'session', session: { snapshotEvents: () => events }, ctx: { on() { return () => {} } } }
  let invoked = false
  const service = { continued: () => [{ callId: 'call', questions: [{ id: 'q', question: 'Choose' }] }], answer() { invoked = true; return true } }
  const receipts = { read: () => null, submit(agent, request, call, answer, invoke, authorize) { authorize(); return invoke() } }
  const bridge = createInteractions({ isLive: () => true, questionService: service, answerReceipts: receipts })
  bridge.bind(agent, { generation: 1, origins: [], lease_ms: 10000 })
  assert.equal(bridge.continued(agent, 1, true).length, 1)
  assert.equal(bridge.continued(agent, 1).length, 0)
  assert.throws(() => bridge.answerContinued(agent, 1, 'call', 'reply', { answers: [{ id: 'q', selected: [], custom: 'yes' }] }), /interaction_inactive/)
  bridge.bind(agent, { generation: 1, origins: ['remote'], lease_ms: 10000 })
  assert.throws(() => bridge.answerContinued(agent, 1, 'call', 'reply', { answers: [{ id: 'q', selected: [], custom: 'yes' }] }, true), /answer_unconfirmed/)
  assert.equal(invoked, false)
  bridge.dispose()
})
