import test from 'node:test'
import assert from 'node:assert/strict'
import { summarize } from './summary.mjs'

function context(chunks, check = () => {}) {
  return { llm: { async prepareCall(route, signal, select) {
    assert.deepEqual(route, { provider: 'native', model: 'model', maxTokens: 512 })
    assert.ok(signal instanceof AbortSignal)
    assert.equal(select({}, { reasoning: { efforts: [{ id: 'off' }] } }).reasoningEffort, 'off')
    return { config: route, async *stream(options) { check(options); yield* chunks } }
  } } }
}
const route = { provider: 'native', model: 'model' }

test('native auxiliary summary offers zero tools and no original session', async () => {
 const ctx = context([{ type: 'text-delta', index: 0, text: '结论尚不确定，无需回复。' }, { type: 'finish', reason: { kind: 'stop' } }], options => {
  assert.deepEqual(options.tools, [])
  assert.equal(options.sessionId, undefined)
  assert.equal(options.messages.length, 1)
  assert.equal(JSON.parse(options.messages[0].content[0].text).material, '原结论')
 })
 assert.equal(await summarize(ctx, route, '原结论'), '结论尚不确定，无需回复。')
})

test('native summary rejects tool attempts, missing finish and output overflow', async () => {
 for (const chunks of [
  [{ type: 'tool-call-delta', name: 'bash', argumentsDelta: '{}' }],
  [{ type: 'text-delta', index: 0, text: 'unfinished' }],
  [{ type: 'text-delta', index: 0, text: 'x'.repeat(4001) }],
  [{ type: 'text-delta', index: 0, text: 'x' }, { type: 'finish', reason: { kind: 'max-tokens' } }],
 ]) await assert.rejects(summarize(context(chunks), route, 'material'))
 await assert.rejects(summarize(context([]), route, 'x'.repeat(262145)))
})

test('bridge summary preserves the actual desktop provider and original history', async () => {
 const { createBridge } = await import('./bridge.mjs')
 const actual = { provider: 'openai', model: 'gpt-6.1-terra' }
 const ctx = { llm: { async prepareCall(selected) {
  assert.equal(selected.provider, actual.provider)
  assert.equal(selected.model, actual.model)
  return { config:selected, async *stream(options) {
   assert.equal(options.sessionId, undefined)
   assert.deepEqual(options.tools, [])
   yield {type:'text-delta',index:0,text:'已完成，无需回复。'}
   yield {type:'finish',reason:{kind:'stop'}}
  } }
 } } }
 const events = []
 const agent = { id:'original', status:'idle', model:{current:()=>actual}, session:{header:{cwd:'/original'},snapshotEvents:()=>events}, inbox:{nextTurn:[],nextStep:[]} }
 ctx.agents = {get:()=>agent,roots:()=>[agent]}
 const bridge = createBridge(ctx)
 const target = await bridge.dispatch('inspect',{session_id:'original'})
 const result = await bridge.dispatch('summarize',{...target,text:'已完成'})
 assert.equal(result.provider,'openai')
 assert.equal(result.model,'gpt-6.1-terra')
 assert.equal(events.length,0)
 assert.equal(agent.inbox.nextTurn.length,0)
 assert.equal(agent.inbox.nextStep.length,0)
 bridge.dispose()
})
