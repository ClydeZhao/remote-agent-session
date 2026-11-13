import { randomUUID } from 'node:crypto'
import { continuedQuestions } from './continued.mjs'

const internalSources = new Set(['runtime-context', 'skill-catalog', 'time-context'])

export function turnSources(agent, replyOrigin = () => null) {
  let turn = ''
  let ids = []
  for (const event of agent.session.snapshotEvents()) {
    if (event.type === 'turn/start') {
      turn = String(event.data.turn)
      ids = []
    } else if (event.type === 'turn/end') {
      turn = ''
      ids = []
    } else if (event.type === 'user/message' && turn) {
      const source = event.data.source
      if (!internalSources.has(source?.kind)) ids.push(source?.kind === 'user' ? source.rpcId ?? '' : replyOrigin(agent.id, event.data) ?? '')
    }
  }
  return { turn, ids }
}

function validateAnswers(questions, value) {
      if (!Array.isArray(value?.answers) || value.answers.length !== questions.length || new Set(value.answers.map(answer => answer.id)).size !== questions.length) throw new Error('invalid_answer')
      for (const answer of value.answers) {
        const question = questions.find(question => question.id === answer.id)
        if (!question || !Array.isArray(answer.selected) || answer.selected.some(label => !question.options?.some(option => option.label === label)) || new Set(answer.selected).size !== answer.selected.length || (!question.multiSelect && answer.selected.length > 1) || (answer.custom !== undefined && (typeof answer.custom !== 'string' || answer.custom.length > 8000)) || (!question.multiSelect && answer.custom && answer.selected.length)) throw new Error('invalid_answer')
      }
}

export function createInteractions({ now = Date.now, isLive, deadlineMs = 300000, questionService, answerReceipts }) {
  const bindings = new Map()
  const requests = new Map()
  const listeners = new Map()

  function remote(agent, binding) {
    if (!binding || bindings.get(agent) !== binding || binding.expires <= now() || !isLive(agent)) return null
    const source = turnSources(agent, answerReceipts?.origin)
    return source.turn && source.ids.length && source.ids.every(id => id && binding.origins.has(id)) ? source : null
  }

  function withdraw(agent, generation) {
    if (generation !== undefined && bindings.get(agent)?.generation !== generation) throw new Error('stale_binding')
    bindings.delete(agent)
    for (const item of requests.values()) if (item.agent === agent) item.finish(false)
    for (const dispose of listeners.get(agent) ?? []) dispose()
    listeners.delete(agent)
  }

  function hold(agent, binding, source, native, kind, payload) {
    if (native.signal?.aborted) return Promise.reject(new Error('interaction_aborted'))
    if (requests.size >= 64) return Promise.reject(new Error('interaction_capacity'))
    const id = randomUUID()
    return new Promise((resolve, reject) => {
      let settled = false
      const abort = () => finish(false)
      const timer = setTimeout(abort, deadlineMs)
      const leaseTimer = setInterval(() => { if (!remote(agent, binding)) abort() }, 1000)
      leaseTimer.unref?.()
      timer.unref?.()
      function finish(accepted, answer) {
        if (settled) return
        settled = true
        clearTimeout(timer)
        clearInterval(leaseTimer)
        native.signal?.removeEventListener('abort', abort)
        requests.delete(id)
        if (accepted) resolve(answer)
        else if (kind === 'permission') resolve('rejected')
        else reject(new Error('interaction_aborted'))
      }
      requests.set(id, { id, agent, binding, source, native, kind, payload, finish })
      native.signal?.addEventListener('abort', abort, { once: true })
      if (native.signal?.aborted) abort()
    })
  }

  function bind(agent, { generation, origins, lease_ms }) {
    if (!Number.isSafeInteger(generation) || generation < 1 || !Array.isArray(origins) || origins.length > 100 || origins.some(id => typeof id !== 'string' || !id || id.length > 200) || !Number.isInteger(lease_ms) || lease_ms < 1000 || lease_ms > 30000) throw new Error('invalid_binding')
    const old = bindings.get(agent)
    if (old && old.generation > generation) throw new Error('stale_binding')
    if (old && old.generation === generation) {
      const combined = new Set([...old.origins, ...origins])
      if (combined.size > 4096) throw new Error('binding_capacity')
      old.origins = combined
      old.expires = now() + lease_ms
      return
    }
    withdraw(agent)
    const binding = { generation, origins: new Set(origins), expires: now() + lease_ms }
    bindings.set(agent, binding)
    const permission = agent.ctx.on('approval/request', (request, next) => {
      const source = remote(agent, binding)
      if (!source || request.agent !== agent) return next()
      const events = agent.session.snapshotEvents()
      const start = events.findLastIndex(event => event.type === 'turn/start')
      const call = events.slice(start + 1).findLast(event => event.type === 'tool/call' && event.data.callId === request.callId)
      if (!request.callId || !call) return 'rejected'
      return hold(agent, binding, source, request, 'permission', { tool_name: request.toolName, reason: request.reason ?? '', call_id: request.callId, tool_call: call.data })
    }, true)
    const question = agent.ctx.on('user-questions/request', (request, next) => {
      const source = remote(agent, binding)
      if (!source || request.agent !== agent) return next()
      if (!Array.isArray(request.questions) || request.questions.some(question => question.isSecret || question.secret || question.intent?.kind === 'approval')) return Promise.reject(new Error('unsupported_remote_question'))
      return hold(agent, binding, source, request, 'question', { questions: request.questions, timed: request.wait?.timed === true })
    }, true)
    listeners.set(agent, [permission, question])
  }

  function valid(item) {
    const source = remote(item.agent, item.binding)
    return !item.native.signal?.aborted && source && source.turn === item.source.turn && JSON.stringify(source.ids) === JSON.stringify(item.source.ids)
  }

  function list(agent, generation) {
    const result = []
    for (const item of requests.values()) {
      if (item.agent !== agent) continue
      if (!valid(item)) { item.finish(false); continue }
      if (item.binding.generation === generation) result.push({ id: item.id, kind: item.kind, turn_id: item.source.turn, origin_ids: item.source.ids, ...item.payload })
    }
    return result
  }

  function answer(agent, generation, id, value) {
    const item = requests.get(id)
    if (!item || item.agent !== agent || item.binding.generation !== generation || !valid(item)) {
      if (item?.agent === agent && !valid(item)) item.finish(false)
      throw new Error('interaction_inactive')
    }
    if (item.kind === 'permission') {
      if (value !== 'allow' && value !== 'deny') throw new Error('invalid_decision')
      item.finish(true, value === 'allow' ? 'allowed-once' : 'rejected')
    } else {
      validateAnswers(item.native.questions, value)
      item.finish(true, { answers: value.answers.map(({ id, selected, custom }) => ({ id, selected, ...(custom === undefined ? {} : { custom }) })) })
    }
    return { accepted: true }
  }

  function dispose() {
    for (const agent of [...bindings.keys()]) withdraw(agent)
  }

  function continued(agent, generation, candidates = false) {
    const binding = bindings.get(agent)
    if (!binding || binding.generation !== generation || binding.expires <= now() || !isLive(agent) || !questionService) return []
    return continuedQuestions(agent, questionService, candidates ? null : binding.origins, answerReceipts?.origin)
  }

  function answerContinued(agent, generation, callId, requestId, value, verifyOnly = false) {
    if (!answerReceipts || !questionService) throw new Error('continued_answer_unavailable')
    if (verifyOnly && !answerReceipts.read(agent.id, requestId)) throw new Error('answer_unconfirmed')
    const authorized = () => {
      const question = continued(agent, generation).find(question => question.call_id === callId)
      if (!question) throw new Error('interaction_inactive')
      validateAnswers(question.questions, value)
    }
    const binding = bindings.get(agent)
    if (!binding || binding.generation !== generation || binding.expires <= now() || !isLive(agent)) throw new Error('interaction_inactive')
    return answerReceipts.submit(agent, requestId, callId, value, () => questionService.answer(agent, callId, value), authorized)
  }

  return { bind, withdraw, list, answer, continued, answerContinued, dispose }
}
