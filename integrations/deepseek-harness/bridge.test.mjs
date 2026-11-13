import { test } from 'node:test'
import assert from 'node:assert/strict'
import { mkdtempSync, rmSync } from 'node:fs'
import { createBridge } from './bridge.mjs'
import { createAnswerReceipts } from './answer-receipts.mjs'

function fixture(answerReceipts) {
  let events = []
  let created = 0
  let resumed = 0
  const agent = {
    id: 'session-test',
    status: 'idle',
    session: { header: { cwd: '/test/workspace' }, snapshotEvents: () => events },
    inbox: { nextTurn: [], nextStep: [], remove(id) { this.nextTurn = this.nextTurn.filter(message => message.id !== id); this.nextStep = this.nextStep.filter(message => message.id !== id) } },
    followup(message) {
      assert.equal(message.role, 'user')
      assert.equal(typeof message.id, 'string')
      assert.notEqual(message.id, '')
      const entry = message
      events.push({ seq: events.length, type: 'agent/inbox/spliced', data: { inserted: [entry] } })
      this.inbox.nextTurn.push(entry)
    },
    cancel(cause, options) { this.cancelled = { cause, options }; this.status = 'idle' },
    whenIdle: async () => {},
  }
  let live = agent
  const registry = {
    get: id => id === agent.id ? live : undefined,
    roots: () => live ? [live] : [],
    create: () => { created++ },
    resume: () => { resumed++ },
  }
  const bridge = createBridge({ agents: registry }, 'instance-test', answerReceipts)
  return { agent, bridge, registry, replace: value => { live = value }, events: value => { events = value }, counts: () => ({ created, resumed }) }
}

test('unloaded target is never created or resumed', async () => {
  const f = fixture()
  f.replace(undefined)
  await assert.rejects(f.bridge.dispatch('inspect', { session_id: 'session-test' }), { code: 'owner_unavailable' })
  assert.deepEqual(f.counts(), { created: 0, resumed: 0 })
})

test('same session ID with a replacement owner invalidates controls', async () => {
  const f = fixture()
  const target = await f.bridge.dispatch('inspect', { session_id: f.agent.id })
  f.replace({ ...f.agent })
  await assert.rejects(f.bridge.dispatch('submit', { ...target, request_id: 'r1', text: 'work' }), { code: 'owner_changed' })
  assert.equal(f.agent.inbox.nextTurn.length, 0)
})

test('retry and withdrawal preserve input identity without repeating work', async () => {
  const f = fixture()
  const target = await f.bridge.dispatch('inspect', { session_id: f.agent.id })
  const request = { ...target, request_id: 'r1', text: 'work' }
  await f.bridge.dispatch('submit', request)
  await f.bridge.dispatch('submit', request)
  assert.equal(f.agent.inbox.nextTurn.length, 1)
  await assert.rejects(f.bridge.dispatch('submit', { ...request, text: 'different work' }), { code: 'request_conflict' })
  await f.bridge.dispatch('cancel_queued', request)
  await f.bridge.dispatch('submit', request)
  assert.equal(f.agent.inbox.nextTurn.length, 0)
})

for (const queue of ['nextTurn', 'nextStep']) {
  test('withdrawal removes only the authenticated continued answer in ' + queue, async t => {
    const directory = mkdtempSync('/tmp/bb-dsh-answer-cancel-')
    t.after(() => rmSync(directory, { recursive: true, force: true }))
    const receipts = createAnswerReceipts(directory)
    const f = fixture(receipts)
    const answer = { answers: [{ question: 'Choice', answer: 'blue' }] }
    const reply = { id: 'remote-native-answer', source: { kind: 'user-question-reply', callId: 'question-call', outcome: 'answered' }, content: [{ type: 'text', text: JSON.stringify({ kind: 'answer_to_pending_question', callId: 'question-call', answers: answer.answers }) }] }
    receipts.submit(f.agent, 'remote-request', 'question-call', answer, () => { f.agent.inbox[queue].push(reply); return true }, () => {})
    const local = { ...reply, id: 'local-answer' }
    const other = { id: 'other-request', source: { kind: 'user', rpcId: 'other' } }
    f.agent.inbox[queue].push(local)
    f.agent.inbox[queue === 'nextTurn' ? 'nextStep' : 'nextTurn'].push(other)
    const target = await f.bridge.dispatch('inspect', { session_id: f.agent.id })
    const params = { ...target, request_id: 'remote-request' }
    assert.equal((await f.bridge.dispatch('queued', params)).queued, true)
    await assert.rejects(f.bridge.dispatch('cancel_queued', { ...params, owner: 'stale' }), { code: 'owner_changed' })
    assert.equal((await f.bridge.dispatch('queued', params)).queued, true)
    assert.deepEqual(await f.bridge.dispatch('cancel_queued', params), { cancelled: true })
    assert.equal((await f.bridge.dispatch('queued', params)).queued, false)
    assert.deepEqual(f.agent.inbox[queue], [local])
    assert.deepEqual(f.agent.inbox[queue === 'nextTurn' ? 'nextStep' : 'nextTurn'], [other])
  })
}

test('withdrawal never confirms removal when the host leaves the request queued', async () => {
  const f = fixture()
  const target = await f.bridge.dispatch('inspect', { session_id: f.agent.id })
  const params = { ...target, request_id: 'not-removed', text: 'work' }
  await f.bridge.dispatch('submit', params)
  f.agent.inbox.remove = () => {}
  assert.deepEqual(await f.bridge.dispatch('cancel_queued', params), { cancelled: false })
  assert.equal((await f.bridge.dispatch('queued', params)).queued, true)
})

test('submitted messages remain identified and replayable after persistence and retry', async () => {
  const f = fixture()
  const target = await f.bridge.dispatch('inspect', { session_id: f.agent.id })
  const request = { ...target, request_id: 'persistent-input', text: 'synthetic input' }
  await f.bridge.dispatch('submit', request)
  const original = f.agent.inbox.nextTurn[0]
  const replay = JSON.parse(JSON.stringify({ seq: 25, type: 'user/message', data: original }))
  assert.equal(replay.data.role, 'user')
  assert.equal(typeof replay.data.id, 'string')
  assert.notEqual(replay.data.id, '')
  assert.equal(replay.data.source.kind, 'user')
  assert.equal(replay.data.source.rpcId, request.request_id)
  assert.deepEqual(replay.data.content, [{ type: 'text', text: request.text }])
  f.events([replay])
  f.agent.inbox.nextTurn = []
  await f.bridge.dispatch('submit', request)
  assert.equal(f.agent.inbox.nextTurn.length, 0)
  assert.equal(replay.data.id, original.id)
})

test('interrupt preserves unrelated pending inputs and checks exact turn', async () => {
  const f = fixture()
  f.events([{ seq: 0, type: 'turn/start', data: { turn: 7 } }])
  f.agent.status = 'running'
  const target = await f.bridge.dispatch('inspect', { session_id: f.agent.id })
  await assert.rejects(f.bridge.dispatch('interrupt', { ...target, turn_id: '6' }), { code: 'turn_changed' })
  assert.equal(f.agent.cancelled, undefined)
  await f.bridge.dispatch('interrupt', target)
  assert.deepEqual(f.agent.cancelled, { cause: { kind: 'user' }, options: { keepInbox: true } })
})

test('history keeps sequence coverage without tool payloads or reasoning', async () => {
  const f = fixture()
  f.events([
    { seq: 0, type: 'tool/call', data: { arguments: 'sensitive tool argument' } },
    { seq: 1, type: 'assistant/message', data: { turn: 1, message: { role: 'assistant', content: [{ type: 'reasoning', text: 'private thought' }, { type: 'text', text: 'public answer' }], source: { credentials: 'never forward' } } } },
  ])
  const target = await f.bridge.dispatch('inspect', { session_id: f.agent.id })
  const history = await f.bridge.dispatch('history', target)
  assert.equal(history.next_seq, 2)
  assert.deepEqual(history.events.map(event => event.seq), [0, 1])
  const output = JSON.stringify(history)
  for (const secret of ['sensitive tool argument', 'private thought', 'never forward']) assert.equal(output.includes(secret), false)
  assert.equal(output.includes('public answer'), true)
})

test('permission policy exposes only physical roots for the exact live owner', async t => {
  const { mkdtempSync, mkdirSync, rmSync, symlinkSync } = await import('node:fs')
  const { realpathSync } = await import('node:fs')
  const root = realpathSync(mkdtempSync('/tmp/bb-dsh-policy-'))
  t.after(() => rmSync(root, { recursive: true, force: true }))
  const profile = root + '/profiles/desktop'
  const state = root + '/state'
  mkdirSync(profile, { recursive: true, mode: 0o700 })
  mkdirSync(state, { mode: 0o700 })
  const agent = { id: 'policy-session', status: 'idle', session: { header: { cwd: root }, snapshotEvents: () => [] } }
  let resolved = { mode: 'workspace-write', workspaceRoot: root, additionalWritableRoots: [state] }
  const bridge = createBridge({
    agents: { get: () => agent, roots: () => [agent] },
    profileContext: { dir: profile, name: 'desktop', other: 'private-profile-value' },
    sandboxPolicy: { resolve({ session }) { assert.equal(session, agent.session); return resolved } },
  })
  const target = await bridge.dispatch('inspect', { session_id: agent.id })
  const result = await bridge.dispatch('permission_policy', target)
  assert.deepEqual(result, { mode: 'workspace-write', profile: 'desktop', profile_directory: profile, workspace_root: root, additional_writable_roots: [state] })
  await assert.rejects(bridge.dispatch('permission_policy', { ...target, owner: 'stale' }), { code: 'owner_changed' })
  symlinkSync(state, root + '/alias')
  resolved = { ...resolved, additionalWritableRoots: [root + '/alias'] }
  await assert.rejects(bridge.dispatch('permission_policy', target), { code: 'permission_policy_unavailable' })
})

test('permission policy never invents empty roots when policy service is missing', async () => {
  const f = fixture()
  const target = await f.bridge.dispatch('inspect', { session_id: f.agent.id })
  await assert.rejects(f.bridge.dispatch('permission_policy', target), { code: 'permission_policy_unavailable' })
})

for (const origin of [undefined, 'subagent', 'unknown']) {
  test('registry without roots admits only ordinary sessions: ' + String(origin), async () => {
    const f = fixture()
    delete f.registry.roots
    f.agent.session.header.origin = origin
    f.agent.session.header.parentSession = 'historical-parent'
    if (origin === undefined) {
      const target = await f.bridge.dispatch('inspect', { session_id: f.agent.id })
      await f.bridge.dispatch('submit', { ...target, request_id: 'ordinary', text: 'work' })
      assert.equal(f.agent.inbox.nextTurn.length, 1)
      f.agent.session.header.origin = 'subagent'
      await assert.rejects(f.bridge.dispatch('history', target), { code: 'owner_unavailable' })
    } else {
      await assert.rejects(f.bridge.dispatch('inspect', { session_id: f.agent.id }), { code: 'owner_unavailable' })
      assert.equal(f.agent.inbox.nextTurn.length, 0)
    }
  })
}

test('legacy runtime ownership remains authoritative', async () => {
  const f = fixture()
  f.registry.roots = () => []
  await assert.rejects(f.bridge.dispatch('inspect', { session_id: f.agent.id }), { code: 'owner_unavailable' })
})
