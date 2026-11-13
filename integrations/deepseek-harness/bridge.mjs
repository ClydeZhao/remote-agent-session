import { randomUUID } from 'node:crypto'
import { lstatSync, realpathSync } from 'node:fs'
import { isAbsolute } from 'node:path'
import { summarize } from './summary.mjs'
import { createInteractions } from './interactions.mjs'

export class BridgeError extends Error {
  constructor(code) {
    super(code)
    this.code = code
  }
}

function requireText(value) {
  if (typeof value !== 'string' || value.length === 0 || value.length > 262144) throw new BridgeError('invalid_request')
  return value
}

function pending(agent) {
  return [...agent.inbox.nextTurn, ...agent.inbox.nextStep]
}

function requestOrigin(agent, message, receipts) {
  return message.source?.kind === 'user' ? message.source.rpcId : receipts?.origin(agent.id, message)
}

function activeTurn(agent) {
  const events = agent.session.snapshotEvents()
  for (let i = events.length - 1; i >= 0; i--) {
    if (events[i].type === 'turn/end') return ''
    if (events[i].type === 'turn/start') return String(events[i].data.turn)
  }
  return ''
}

function historyEvent(event, session, receipts) {
  let data = {}
  if (event.type === 'turn/start') data = { turn: event.data.turn }
  if (event.type === 'turn/end') data = { turn: event.data.turn, reason: { kind: event.data.reason?.kind } }
  if (event.type === 'user/message') {
    const remote = receipts?.origin(session, event.data)
    data = { source: remote ? { kind: 'user', rpcId: remote } : { kind: event.data.source?.kind, rpcId: event.data.source?.rpcId } }
  }
  if (event.type === 'assistant/message') data = {
    turn: event.data.turn,
    message: {
      role: event.data.message?.role,
      content: event.data.message?.content?.filter(block => block.type === 'text').map(block => ({ type: 'text', text: block.text })),
    },
  }
  return { seq: event.seq, type: event.type, data }
}

export function createBridge(ctx, instance = randomUUID(), answerReceipts) {
  const owners = new WeakMap()
  let summaryRunning = false
  function isLive(agent) {
    if (!agent || ctx.agents.get(agent.id) !== agent) return false
    return typeof ctx.agents.roots === 'function'
      ? ctx.agents.roots().includes(agent)
      : agent.session.header.origin === undefined
  }

  const interactions = createInteractions({ isLive, questionService: ctx.userQuestions, answerReceipts })

  function target(params) {
    const agent = ctx.agents.get(requireText(params.session_id))
    if (!isLive(agent)) throw new BridgeError('owner_unavailable')
    if (!owners.has(agent)) owners.set(agent, randomUUID())
    if (params.instance !== instance || params.owner !== owners.get(agent) || params.cwd !== agent.session.header.cwd) throw new BridgeError('owner_changed')
    return agent
  }

  async function dispatch(method, params) {
    if (method === 'capabilities') return { protocol: 1, interactions: true, continued: Boolean(ctx.userQuestions && answerReceipts), summary: Boolean(ctx.llm) }
    if (method === 'inspect') {
      const agent = ctx.agents.get(requireText(params.session_id))
      if (!isLive(agent)) throw new BridgeError('owner_unavailable')
      if (!owners.has(agent)) owners.set(agent, randomUUID())
      return { instance, owner: owners.get(agent), session_id: agent.id, cwd: agent.session.header.cwd, status: agent.status, turn_id: activeTurn(agent) }
    }
    const agent = target(params)
    if (method === 'summarize') {
      if (summaryRunning) throw new BridgeError('summary_busy')
      summaryRunning = true
      try {
        const route = agent.model.current()
        return { summary: await summarize(ctx, route, params.text), provider: route.provider, model: route.model }
      } finally { summaryRunning = false }
    }
    if (method === 'permission_policy') {
      const service = ctx.sandboxPolicy
      const profile = ctx.profileContext
      if (!service || !profile?.dir || !profile.name) throw new BridgeError('permission_policy_unavailable')
      const physical = path => {
        if (!isAbsolute(path) || lstatSync(path).isSymbolicLink() || !lstatSync(path).isDirectory()) throw new BridgeError('permission_policy_unavailable')
        return realpathSync(path)
      }
      const policy = service.resolve({ session: agent.session })
      if (!['read-only', 'workspace-write', 'danger-full-access'].includes(policy.mode) || !Array.isArray(policy.additionalWritableRoots ?? [])) throw new BridgeError('permission_policy_unavailable')
      return { mode: policy.mode, profile: profile.name, profile_directory: physical(profile.dir), workspace_root: physical(policy.workspaceRoot), additional_writable_roots: (policy.additionalWritableRoots ?? []).map(physical) }
    }
    if (method === 'bind_interactions') {
      interactions.bind(agent, params)
      return { bound: true }
    }
    if (method === 'unbind_interactions') {
      interactions.withdraw(agent, params.generation)
      return { unbound: true }
    }
    if (method === 'interactions') return { requests: interactions.list(agent, params.generation) }
    if (method === 'answer_continued') return interactions.answerContinued(agent, params.generation, requireText(params.call_id), requireText(params.request_id), params.answer, params.verify_only === true)
    if (method === 'continued_interactions') return { requests: interactions.continued(agent, params.generation, params.candidates === true) }
    if (method === 'answer_interaction') return interactions.answer(agent, params.generation, params.interaction_id, params.answer)
    if (method === 'history') {
      const events = agent.session.snapshotEvents()
      return { session_id: agent.id, next_seq: events.length, events: events.map(event => historyEvent(event, agent.id, answerReceipts)) }
    }
    if (method === 'queued') {
      const id = requireText(params.request_id)
      return { queued: pending(agent).some(message => requestOrigin(agent, message, answerReceipts) === id) }
    }
    if (method === 'interrupt') {
      if (agent.status === 'idle') return { interrupted: true }
      if (!params.turn_id || activeTurn(agent) !== params.turn_id) throw new BridgeError('turn_changed')
      agent.cancel({ kind: 'user' }, { keepInbox: true })
      await agent.whenIdle()
      target(params)
      return { interrupted: true }
    }
    if (method === 'submit') {
      const id = requireText(params.request_id)
      const text = requireText(params.text)
      const logged = agent.session.snapshotEvents().filter(event => event.type === 'user/message' || event.type === 'agent/inbox/spliced').flatMap(event => event.type === 'user/message' ? [event.data] : event.data.inserted ?? [])
      const prior = [...logged, ...pending(agent)].find(message => message.source?.kind === 'user' && message.source.rpcId === id)
      if (prior) {
        if (JSON.stringify(prior.content) !== JSON.stringify([{ type: 'text', text }])) throw new BridgeError('request_conflict')
        return { accepted: true }
      }
      if (agent.status !== 'idle' || activeTurn(agent) !== '') throw new BridgeError('agent_busy')
      const content = Object.freeze([Object.freeze({ type: 'text', text })])
      const source = Object.freeze({ kind: 'user', rpcId: id })
      agent.followup(Object.freeze({ id: randomUUID(), role: 'user', content, source }))
      return { accepted: true }
    }
    if (method === 'cancel_queued') {
      const id = requireText(params.request_id)
      for (const message of pending(agent)) {
        if (requestOrigin(agent, message, answerReceipts) !== id) continue
        target(params)
        agent.inbox.remove(message.id)
      }
      target(params)
      return { cancelled: !pending(agent).some(message => requestOrigin(agent, message, answerReceipts) === id) }
    }
    throw new BridgeError('unsupported')
  }

  return { instance, dispatch, dispose: interactions.dispose }
}
