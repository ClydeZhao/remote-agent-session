import { createHash, randomUUID } from 'node:crypto'
import { closeSync, constants, fsyncSync, lstatSync, openSync, readFileSync, renameSync, unlinkSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'

function messages(agent) {
  return [
    ...agent.session.snapshotEvents().flatMap(event => event.type === 'user/message' ? [event.data] : event.type === 'agent/inbox/spliced' ? event.data.inserted ?? [] : []),
    ...agent.inbox.nextTurn,
    ...agent.inbox.nextStep,
  ]
}

function replyMatches(message, callId, answer) {
  if (message.source?.kind !== 'user-question-reply' || message.source.callId !== callId || message.source.outcome !== 'answered') return false
  if (!Array.isArray(message.content) || message.content.length !== 1 || message.content[0].type !== 'text') return false
  try {
    const body = JSON.parse(message.content[0].text)
    return body.kind === 'answer_to_pending_question' && body.callId === callId && JSON.stringify(body.answers) === JSON.stringify(answer.answers)
  } catch { return false }
}

export function createAnswerReceipts(directory) {
  const metadata = lstatSync(directory)
  if (!metadata.isDirectory() || metadata.isSymbolicLink() || metadata.uid !== process.getuid() || metadata.mode & 0o077) throw new Error('unsafe_receipt_directory')

  function path(session, request) {
    const digest = createHash('sha256').update(JSON.stringify([session, request])).digest('hex')
    return join(directory, 'answer-' + digest + '.json')
  }

  function readFile(file) {
    let stat
    try { stat = lstatSync(file) } catch (error) { if (error.code === 'ENOENT') return null; throw error }
    if (!stat.isFile() || stat.isSymbolicLink() || stat.nlink !== 1 || stat.uid !== process.getuid() || stat.mode & 0o077 || stat.size > 1048576) throw new Error('unsafe_receipt')
    const descriptor = openSync(file, constants.O_RDONLY | constants.O_NOFOLLOW)
    try { return JSON.parse(readFileSync(descriptor, 'utf8')) } finally { closeSync(descriptor) }
  }

  function read(session, request) { return readFile(path(session, request)) }

  function write(receipt, destination = path(receipt.session, receipt.request)) {
    const temporary = destination + '.' + randomUUID() + '.tmp'
    const data = JSON.stringify(receipt)
    if (Buffer.byteLength(data) > 1048576) throw new Error('answer_too_large')
    const descriptor = openSync(temporary, constants.O_WRONLY | constants.O_CREAT | constants.O_EXCL, 0o600)
    try {
      writeFileSync(descriptor, data)
      fsyncSync(descriptor)
    } finally { closeSync(descriptor) }
    try {
      renameSync(temporary, destination)
      const parent = openSync(directory, constants.O_RDONLY)
      try { fsyncSync(parent) } finally { closeSync(parent) }
    } finally { try { unlinkSync(temporary) } catch {} }
  }

  function submit(agent, request, callId, answer, invoke, authorize) {
    const previous = read(agent.id, request)
    if (previous) {
      if (previous.session !== agent.id || previous.request !== request || previous.callId !== callId || JSON.stringify(previous.answer) !== JSON.stringify(answer)) throw new Error('answer_conflict')
      if (previous.state !== 'accepted') throw new Error('answer_unconfirmed')
      const found = messages(agent).some(message => message.id === previous.messageId && replyMatches(message, callId, answer))
      if (!found) throw new Error('answer_unconfirmed')
      write(previous, nativePath(agent.id, previous.messageId))
      return { accepted: true, message_id: previous.messageId }
    }
    if (messages(agent).some(message => message.source?.kind === 'user-question-reply' && message.source.callId === callId)) throw new Error('question_already_answered')
    authorize()
    const receipt = { session: agent.id, request, callId, answer, state: 'dispatching' }
    write(receipt)
    if (invoke() !== true) throw new Error('answer_unconfirmed')
    const candidates = new Map(messages(agent).filter(message => replyMatches(message, callId, answer)).map(message => [message.id, message]))
    if (candidates.size !== 1 || ![...candidates.keys()][0]) throw new Error('answer_unconfirmed')
    receipt.state = 'accepted'
    receipt.messageId = [...candidates.keys()][0]
    write(receipt)
    write(receipt, nativePath(agent.id, receipt.messageId))
    return { accepted: true, message_id: receipt.messageId }
  }

  function nativePath(session, messageId) {
    const digest = createHash('sha256').update(JSON.stringify([session, messageId])).digest('hex')
    return join(directory, 'native-' + digest + '.json')
  }

  function origin(session, message) {
    if (message.source?.kind !== 'user-question-reply' || !message.id) return null
    const receipt = readFile(nativePath(session, message.id))
    if (!receipt || receipt.state !== 'accepted' || receipt.session !== session || receipt.messageId !== message.id || !replyMatches(message, receipt.callId, receipt.answer)) return null
    const original = read(session, receipt.request)
    if (JSON.stringify(original) !== JSON.stringify(receipt)) return null
    return receipt.request
  }

  return { submit, read, origin }
}
