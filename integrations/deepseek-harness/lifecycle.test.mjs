import { test } from 'node:test'
import assert from 'node:assert/strict'
import { mkdtemp, readdir, readFile, rm } from 'node:fs/promises'
import { createConnection } from 'node:net'
import { apply } from './index.mjs'
import { createBridge } from './bridge.mjs'
import { createAnswerReceipts } from './answer-receipts.mjs'

test('plugin disposal closes live connections and removes only its own endpoint', async () => {
  const directory = await mkdtemp('/tmp/bb-dsh-lifecycle-')
  const disposers = []
  const ctx = { agents: { get() {}, roots: () => [] }, effect(factory) { disposers.push(factory()) } }
  let socket
  try {
    await apply(ctx, { directory })
    const files = await readdir(directory)
    const descriptor = JSON.parse(await readFile(directory + '/' + files.find(name => name.endsWith('.json')), 'utf8'))
    socket = createConnection(descriptor.socket)
    await new Promise((resolve, reject) => { socket.once('connect', resolve); socket.once('error', reject) })
    const closed = new Promise(resolve => socket.once('close', resolve))
    assert.equal(disposers.length, 1)
    await disposers[0]()
    await closed
    assert.deepEqual(await readdir(directory), ['answers'])
    assert.equal(socket.destroyed, true)
  } finally {
    socket?.destroy()
    await rm(directory, { recursive: true, force: true })
  }
})

async function continuedBridgeFixture(t) {
  const directory = await mkdtemp('/tmp/bb-dsh-continued-')
  const bridges = []
  t.after(async () => {
    for (const bridge of bridges) bridge.dispose()
    await rm(directory, { recursive: true, force: true })
  })
  const pending = new Map(['first', 'second'].map(callId => [callId, { callId, questions: [{ id: callId + '-item', question: 'Choose ' + callId, options: [{ label: 'yes' }, { label: 'no' }] }] }]))
  const events = [
    { type: 'turn/start', data: { turn: 7 } },
    { type: 'user/message', data: { source: { kind: 'user', rpcId: 'remote-input' } } },
    ...[...pending.keys()].map(callId => ({ type: 'tool/call', data: { name: 'ask_user_question', callId } })),
    { type: 'turn/end', data: { turn: 7 } },
  ]
  const delivered = []
  const agent = {
    id: 'original-session', status: 'idle',
    session: { header: { cwd: '/test/workspace' }, snapshotEvents: () => events },
    inbox: { nextTurn: [], nextStep: [] },
    ctx: { on() { return () => {} } },
    followup() { throw new Error('ordinary input must not receive answers') },
    cancel() { throw new Error('continued answer must not interrupt') },
  }
  const userQuestions = {
    continued: () => [...pending.values()],
    answer(target, callId, value) {
      assert.equal(target, agent)
      assert.equal(pending.delete(callId), true)
      delivered.push({ callId, value })
      events.push({ type: 'user/message', data: {
        id: 'native-' + callId,
        source: { kind: 'user-question-reply', callId, outcome: 'answered' },
        content: [{ type: 'text', text: JSON.stringify({ kind: 'answer_to_pending_question', callId, answers: value.answers }) }],
      } })
      return true
    },
  }
  const ctx = { agents: { get: id => id === agent.id ? agent : undefined, roots: () => [agent] }, userQuestions }
  async function connect(generation = 4, origins = ['remote-input']) {
    const bridge = createBridge(ctx, undefined, createAnswerReceipts(directory))
    bridges.push(bridge)
    const target = await bridge.dispatch('inspect', { session_id: agent.id })
    const params = { ...target, generation }
    await bridge.dispatch('bind_interactions', { ...params, origins, lease_ms: 10000 })
    return { bridge, params, request: (method, extra = {}) => bridge.dispatch(method, { ...params, ...extra }) }
  }
  const answer = callId => ({ answers: [{ id: callId + '-item', selected: ['yes'] }] })
  return { connect, agent, events, pending, delivered, answer }
}

test('continued questions survive plugin replacement and keep separate native deliveries', async t => {
  const f = await continuedBridgeFixture(t)
  const original = await f.connect()
  const before = await original.request('continued_interactions')
  assert.deepEqual(before.requests.map(item => item.call_id), ['first', 'second'])
  original.bridge.dispose()
  const restored = await f.connect()
  assert.deepEqual((await restored.request('continued_interactions')).requests, before.requests)
  await assert.rejects(restored.bridge.dispatch('answer_continued', { ...original.params, call_id: 'first', request_id: 'answer-first', answer: f.answer('first') }), { code: 'owner_changed' })
  const first = await restored.request('answer_continued', { call_id: 'first', request_id: 'answer-first', answer: f.answer('first') })
  assert.equal(first.message_id, 'native-first')
  await restored.request('bind_interactions', { origins: ['answer-first'], lease_ms: 10000 })
  const remaining = await restored.request('continued_interactions')
  assert.deepEqual(remaining.requests.map(item => item.call_id), ['second'])
  assert.deepEqual(remaining.requests[0].all_origin_ids, ['remote-input', 'answer-first'])
  assert.deepEqual(remaining.requests[0].origin_ids, ['remote-input'])
  await restored.request('answer_continued', { call_id: 'second', request_id: 'answer-second', answer: f.answer('second') })
  assert.deepEqual(f.delivered.map(item => item.callId), ['first', 'second'])
  assert.deepEqual((await restored.request('continued_interactions')).requests, [])
  const reloaded = await f.connect(4, ['remote-input', 'answer-first', 'answer-second'])
  assert.deepEqual(await reloaded.request('answer_continued', { call_id: 'first', request_id: 'answer-first', answer: f.answer('first'), verify_only: true }), first)
  assert.equal(f.delivered.length, 2)
})

test('explicit detach and a new binding cannot deliver stale continued answers', async t => {
  const f = await continuedBridgeFixture(t)
  const connection = await f.connect()
  await connection.request('unbind_interactions')
  const reply = { call_id: 'first', request_id: 'stale-answer', answer: f.answer('first') }
  await assert.rejects(connection.request('answer_continued', reply), /interaction_inactive/)
  await connection.request('bind_interactions', { generation: 5, origins: ['new-input'], lease_ms: 10000 })
  await assert.rejects(connection.request('answer_continued', reply), /interaction_inactive/)
  await assert.rejects(connection.request('answer_continued', { ...reply, generation: 5 }), /interaction_inactive/)
  assert.deepEqual(f.delivered, [])
  assert.equal(f.pending.size, 2)
})

for (const source of [{ kind: 'user', rpcId: 'later-local-input' }, { kind: 'user-question-reply', callId: 'first', outcome: 'answered' }]) {
  test('source change before answer prevents native delivery: ' + source.kind, async t => {
    const f = await continuedBridgeFixture(t)
    const connection = await f.connect()
    assert.equal((await connection.request('continued_interactions')).requests.length, 2)
    f.events.push({ type: 'user/message', data: { id: 'local-message', source } })
    assert.deepEqual((await connection.request('continued_interactions', { candidates: true })).requests, [])
    await assert.rejects(connection.request('answer_continued', { call_id: 'first', request_id: 'late-answer', answer: f.answer('first') }), /question_already_answered|interaction_inactive/)
    assert.deepEqual(f.delivered, [])
  })
}
