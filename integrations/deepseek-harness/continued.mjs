const internalSources = new Set(['runtime-context', 'skill-catalog', 'time-context'])

export function continuedQuestionOrigins(agent, callId, replyOrigin = () => null) {
  let turn = ''
  let ids = []
  let found
  for (const event of agent.session.snapshotEvents()) {
    if (event.type === 'turn/start') {
      turn = String(event.data.turn)
      ids = []
    }
    if (event.type === 'user/message') {
      const source = event.data.source
      if (internalSources.has(source?.kind)) continue
      const reply = source?.kind === 'user-question-reply' ? replyOrigin(agent.id, event.data) : null
      if (found) {
        if (!reply) return null
        found.allIds.push(reply)
        continue
      }
      if (reply) { ids.push(reply); continue }
      ids.push(source?.kind === 'user' ? source.rpcId ?? '' : '')
    }
    if ((event.type === 'tool/call' && event.data.callId === callId) || (event.type === 'tool/ptc-dispatch' && event.data.subCallId === callId)) {
      if (found || !turn || event.data.name !== 'ask_user_question') return null
      found = { turn, ids: [...ids], allIds: [...ids] }
    }
  }
  if (!found || !found.ids.length || found.ids.some(id => !id)) return null
  return found
}

export function continuedQuestions(agent, service, origins, replyOrigin = () => null) {
  return service.continued(agent).flatMap(question => {
    const source = continuedQuestionOrigins(agent, question.callId, replyOrigin)
    if (!source || (origins && !source.allIds.every(id => origins.has(id)))) return []
    if (question.questions.some(item => item.isSecret || item.secret || item.intent?.kind === 'approval')) return []
    return [{ id: question.callId, kind: 'question', call_id: question.callId, turn_id: source.turn, origin_ids: source.ids, all_origin_ids: source.allIds, questions: question.questions, timed: true, continued: true }]
  })
}
