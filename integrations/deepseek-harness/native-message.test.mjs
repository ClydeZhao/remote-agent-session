import { test } from 'node:test'
import assert from 'node:assert/strict'
import { spawnSync } from 'node:child_process'
import { createBridge } from './bridge.mjs'

test('submitted message passes the selected DSH persistence validator after JSON replay', { skip: !process.env.REMOTE_AGENT_SESSION_TEST_DSH_EXECUTABLE || !process.env.REMOTE_AGENT_SESSION_TEST_DSH_SESSION_MODULE }, async () => {
  let message
  const agent = {
    id: 'synthetic-session', status: 'idle',
    session: { header: { cwd: '/synthetic' }, snapshotEvents: () => [] },
    inbox: { nextTurn: [], nextStep: [] },
    followup(value) { message = value; this.inbox.nextTurn.push(value) },
  }
  const bridge = createBridge({ agents: { get: () => agent, roots: () => [agent] } })
  const target = await bridge.dispatch('inspect', { session_id: agent.id })
  await bridge.dispatch('submit', { ...target, request_id: 'synthetic-request', text: 'synthetic-input' })
  const script = `const chunks=[];for await(const chunk of process.stdin)chunks.push(chunk);const message=JSON.parse(Buffer.concat(chunks));const {adoptSessionEvent}=await import(process.env.REMOTE_AGENT_SESSION_TEST_DSH_SESSION_MODULE);adoptSessionEvent({seq:0,time:0,type:'user/message',surfaceOp:'append',data:message});process.stdout.write('native-message-valid')`
  const result = spawnSync(process.env.REMOTE_AGENT_SESSION_TEST_DSH_EXECUTABLE, ['--input-type=module', '-e', script], {
    input: JSON.stringify(message), encoding: 'utf8', timeout: 15000,
    env: { ...process.env, ELECTRON_RUN_AS_NODE: '1' },
  })
  assert.equal(result.status, 0, 'selected DSH persistence validator rejected the replayed message')
  assert.equal(result.stdout, 'native-message-valid')
})

test('active continued answer uses the selected native service steer without ending original work', { skip: !process.env.REMOTE_AGENT_SESSION_TEST_DSH_EXECUTABLE || !process.env.REMOTE_AGENT_SESSION_TEST_DSH_SESSION_MODULE }, () => {
  const script = async function exerciseNativeContinued() {
    const assert = (await import('node:assert/strict')).default
    const { mkdtempSync, rmSync } = await import('node:fs')
    const { tmpdir } = await import('node:os')
    const { join } = await import('node:path')
    const selected = process.env.REMOTE_AGENT_SESSION_TEST_DSH_SESSION_MODULE
    const sibling = name => new URL('../../' + name + '/lib/index.js', new URL(selected, 'file:///')).href
    const { Context } = await import(sibling('cordis'))
    const { Session, SessionId, adoptSessionEvent } = await import(selected)
    const { createUserMessage } = await import(sibling('dsh-llm'))
    const { default: Projections } = await import(sibling('dsh-session-projection'))
    const { default: Questions } = await import(sibling('dsh-user-questions'))
    const { createBridge } = await import(process.env.REMOTE_AGENT_SESSION_TEST_BRIDGE_MODULE)
    const { createAnswerReceipts } = await import(process.env.REMOTE_AGENT_SESSION_TEST_RECEIPTS_MODULE)
    const ctx = new Context()
    const session = Session.create(SessionId('synthetic-active'), undefined, { version: 4, id: 'synthetic-active', createdAt: 0, isSeeded: false, cwd: '/synthetic' })
    const work = new AbortController()
    let steered = 0
    const agent = {
      id: session.id, status: 'running', session,
      inbox: { nextTurn: [], nextStep: [] }, ctx: { on() { return () => {} } },
      cancel() { assert.fail('continued answer interrupted original work') },
      followup() { assert.fail('continued answer created an ordinary next turn') },
      steer(message) {
        assert.equal(this.status, 'running')
        assert.equal(work.signal.aborted, false)
        adoptSessionEvent({ seq: 0, time: 0, type: 'user/message', surfaceOp: 'append', data: JSON.parse(JSON.stringify(message)) })
        this.inbox.nextStep.push(message)
        steered++
      },
    }
    ctx.provide('agents', { get: id => id === agent.id ? agent : undefined, roots: () => [agent] })
    const directory = mkdtempSync(join(tmpdir(), 'bb-dsh-native-continued-'))
    let bridge
    try {
      await ctx.plugin(Projections)
      await ctx.plugin(Questions)
      session.append('turn/start', { turn: 7 })
      session.append('user/message', createUserMessage({ source: { kind: 'user', rpcId: 'remote' }, content: [{ type: 'text', text: 'synthetic' }] }), { surfaceOp: 'append' })
      session.append('tool/ptc-dispatch', {
        name: 'ask_user_question', subCallId: 'question', callId: 'ptc',
        arguments: { questions: [{ id: 'q', question: 'Choose', options: [{ label: 'yes' }] }] },
        content: [{ type: 'text', text: JSON.stringify({ pending: true, callId: 'question' }) }], isError: false,
      })
      assert.equal(ctx.userQuestions.continued(agent).length, 1)
      bridge = createBridge(ctx, undefined, createAnswerReceipts(directory))
      const target = await bridge.dispatch('inspect', { session_id: agent.id })
      assert.equal(target.turn_id, '7')
      await bridge.dispatch('bind_interactions', { ...target, generation: 4, origins: ['remote', 'answer'], lease_ms: 10000 })
      const reply = { ...target, generation: 4, call_id: 'question', request_id: 'answer', answer: { answers: [{ id: 'q', selected: ['yes'] }] } }
      const result = await bridge.dispatch('answer_continued', reply)
      assert.equal(result.message_id, agent.inbox.nextStep[0].id)
      assert.equal(steered, 1)
      assert.equal(agent.inbox.nextTurn.length, 0)
      assert.equal(agent.status, 'running')
      assert.equal(work.signal.aborted, false)
      assert.equal(session.snapshotEvents().some(event => event.type === 'turn/end'), false)
      assert.equal((await bridge.dispatch('inspect', { session_id: agent.id })).turn_id, '7')
      const message = agent.inbox.nextStep.shift()
      assert.equal(message.role, 'user')
      assert.equal(message.source.kind, 'user-question-reply')
      assert.equal(message.source.callId, 'question')
      session.append('user/message', message, { surfaceOp: 'append' })
      assert.equal(ctx.userQuestions.continued(agent).length, 0)
      assert.deepEqual(await bridge.dispatch('answer_continued', { ...reply, verify_only: true }), result)
      assert.equal(steered, 1)
      assert.equal(agent.status, 'running')
      process.stdout.write('native-active-continued-valid')
    } finally {
      bridge?.dispose()
      await ctx.fiber.dispose()
      rmSync(directory, { recursive: true, force: true })
    }
  }
  const result = spawnSync(process.env.REMOTE_AGENT_SESSION_TEST_DSH_EXECUTABLE, ['--input-type=module', '-e', '(' + script.toString() + ')()'], {
    encoding: 'utf8', timeout: 15000,
    env: {
      ...process.env, ELECTRON_RUN_AS_NODE: '1',
      REMOTE_AGENT_SESSION_TEST_BRIDGE_MODULE: new URL('./bridge.mjs', import.meta.url).href,
      REMOTE_AGENT_SESSION_TEST_RECEIPTS_MODULE: new URL('./answer-receipts.mjs', import.meta.url).href,
    },
  })
  assert.equal(result.status, 0, 'selected native continued-question service rejected active steer behavior')
  assert.equal(result.stdout, 'native-active-continued-valid')
})

test('selected native registry preserves ordinary-session admission and exact owner identity', { skip: !process.env.REMOTE_AGENT_SESSION_TEST_DSH_EXECUTABLE || !process.env.REMOTE_AGENT_SESSION_TEST_DSH_SESSION_MODULE }, () => {
  const script = async function exerciseNativeRegistry() {
    const assert = (await import('node:assert/strict')).default
    const selected = process.env.REMOTE_AGENT_SESSION_TEST_DSH_SESSION_MODULE
    const sibling = name => new URL('../../' + name + '/lib/index.js', new URL(selected, 'file:///')).href
    const { Context } = await import(sibling('cordis'))
    const { Session, SessionId } = await import(selected)
    const { default: Projections } = await import(sibling('dsh-session-projection'))
    const { default: Agents } = await import(sibling('dsh-agent'))
    const { createBridge } = await import(process.env.REMOTE_AGENT_SESSION_TEST_BRIDGE_MODULE)
    const ctx = new Context()
    let bridge
    const disposers = []
    try {
      await ctx.plugin(Projections)
      await ctx.plugin(Agents)
      const ordinary = {
        id: SessionId('ordinary'), status: 'idle', ctx,
        session: Session.create(SessionId('ordinary'), undefined, { version: 4, id: 'ordinary', createdAt: 0, isSeeded: false, cwd: '/synthetic', parentSession: 'historical-parent' }),
        inbox: { nextTurn: [], nextStep: [], remove(id) { this.nextTurn = this.nextTurn.filter(message => message.id !== id) } },
        followup(message) { this.inbox.nextTurn.push(message) },
      }
      const dispose = await ctx.agents.register(ordinary)
      disposers.push(dispose)
      bridge = createBridge(ctx)
      const target = await bridge.dispatch('inspect', { session_id: ordinary.id })
      const request = { ...target, request_id: 'remote-input', text: 'synthetic-input' }
      await bridge.dispatch('submit', request)
      await bridge.dispatch('submit', request)
      assert.equal(ordinary.inbox.nextTurn.length, 1)
      assert.equal((await bridge.dispatch('queued', request)).queued, true)
      await bridge.dispatch('cancel_queued', request)
      assert.equal(ordinary.inbox.nextTurn.length, 0)
      if (typeof ctx.agents.roots !== 'function') {
        const child = { ...ordinary, id: SessionId('child'), session: Session.create(SessionId('child'), undefined, { version: 4, id: 'child', createdAt: 0, isSeeded: false, cwd: '/synthetic', origin: 'subagent', parentSession: ordinary.id }) }
        disposers.push(await ctx.agents.register(child))
        await assert.rejects(bridge.dispatch('inspect', { session_id: child.id }), { code: 'owner_unavailable' })
      }
      await dispose()
      await assert.rejects(bridge.dispatch('history', target), { code: 'owner_unavailable' })
      disposers.push(await ctx.agents.register({ ...ordinary }))
      await assert.rejects(bridge.dispatch('history', target), { code: 'owner_changed' })
      process.stdout.write('native-registry-valid')
    } finally {
      bridge?.dispose()
      for (const dispose of disposers.reverse()) await dispose()
      await ctx.fiber.dispose()
    }
  }
  const result = spawnSync(process.env.REMOTE_AGENT_SESSION_TEST_DSH_EXECUTABLE, ['--input-type=module', '-e', '(' + script.toString() + ')()'], {
    encoding: 'utf8', timeout: 15000,
    env: { ...process.env, ELECTRON_RUN_AS_NODE: '1', REMOTE_AGENT_SESSION_TEST_BRIDGE_MODULE: new URL('./bridge.mjs', import.meta.url).href },
  })
  assert.equal(result.status, 0, result.stderr)
  assert.equal(result.stdout, 'native-registry-valid')
})
