import { test } from 'node:test'
import assert from 'node:assert/strict'
import { mkdtempSync, readdirSync, rmSync, unlinkSync } from 'node:fs'
import { createAnswerReceipts } from './answer-receipts.mjs'

function fixture(t) {
  const directory = mkdtempSync('/tmp/bb-dsh-answer-')
  t.after(() => rmSync(directory, { recursive: true, force: true }))
  const events = []
  const agent = { id: 'session', session: { snapshotEvents: () => events }, inbox: { nextTurn: [], nextStep: [] } }
  const answer = { answers: [{ id: 'q', selected: ['blue'] }] }
  function invoke() {
    agent.inbox.nextStep.push({ id: 'native-id', source: { kind: 'user-question-reply', callId: 'call', outcome: 'answered' }, content: [{ type: 'text', text: JSON.stringify({ kind: 'answer_to_pending_question', callId: 'call', answers: answer.answers }) }] })
    return true
  }
  return { directory, agent, answer, invoke, receipts: createAnswerReceipts(directory) }
}

test('native reply identity survives bridge recreation without resending', t => {
  const f = fixture(t)
  assert.deepEqual(f.receipts.submit(f.agent, 'remote', 'call', f.answer, f.invoke, () => {}), { accepted: true, message_id: 'native-id' })
  const reopened = createAnswerReceipts(f.directory)
  assert.deepEqual(reopened.submit(f.agent, 'remote', 'call', f.answer, () => { throw new Error('resent') }, () => {}), { accepted: true, message_id: 'native-id' })
  assert.throws(() => reopened.submit(f.agent, 'remote', 'call', { answers: [] }, f.invoke, () => {}), /answer_conflict/)
})

test('unknown result never retries native answer or claims acceptance', t => {
  const f = fixture(t)
  assert.throws(() => f.receipts.submit(f.agent, 'remote', 'call', f.answer, () => { f.invoke(); throw new Error('lost receipt') }, () => {}), /lost receipt/)
  const reopened = createAnswerReceipts(f.directory)
  assert.throws(() => reopened.submit(f.agent, 'remote', 'call', f.answer, () => { throw new Error('resent') }, () => {}), /answer_unconfirmed/)
})

test('local reply cannot be adopted as a remote answer', t => {
  const f = fixture(t)
  f.invoke()
  assert.throws(() => f.receipts.submit(f.agent, 'remote', 'call', f.answer, f.invoke, () => {}), /question_already_answered/)
  assert.equal(f.receipts.read('session', 'remote'), null)
})

test('authorization failure records no dispatch and calls no native operation', t => {
  const f = fixture(t)
  assert.throws(() => f.receipts.submit(f.agent, 'remote', 'call', f.answer, f.invoke, () => { throw new Error('expired') }), /expired/)
  assert.equal(f.receipts.read('session', 'remote'), null)
  assert.deepEqual(f.agent.inbox.nextStep, [])
})

test('history attribution requires exact native ID, session, and answer content', t => {
  const f = fixture(t)
  f.receipts.submit(f.agent, 'remote', 'call', f.answer, f.invoke, () => {})
  const message = f.agent.inbox.nextStep[0]
  const reopened = createAnswerReceipts(f.directory)
  assert.equal(reopened.origin('session', message), 'remote')
  assert.equal(reopened.origin('other-session', message), null)
  assert.equal(reopened.origin('session', { ...message, id: 'local-id' }), null)
  assert.equal(reopened.origin('session', { ...message, content: [{ type: 'text', text: JSON.stringify({ kind: 'answer_to_pending_question', callId: 'call', answers: [] }) }] }), null)
})

test('missing reverse index is rebuilt from confirmed receipt without native replay', t => {
  const f = fixture(t)
  f.receipts.submit(f.agent, 'remote', 'call', f.answer, f.invoke, () => {})
  const index = readdirSync(f.directory).find(name => name.startsWith('native-'))
  unlinkSync(f.directory + '/' + index)
  assert.equal(f.receipts.origin('session', f.agent.inbox.nextStep[0]), null)
  f.receipts.submit(f.agent, 'remote', 'call', f.answer, () => { throw new Error('resent') }, () => {})
  assert.equal(f.receipts.origin('session', f.agent.inbox.nextStep[0]), 'remote')
})
