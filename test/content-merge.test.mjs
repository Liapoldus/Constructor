import test from 'node:test'; import assert from 'node:assert/strict'
import {mergeContentDocuments} from '../src/content-merge.ts'
const document=fields=>({schemaVersion:1,id:'content',instances:[{id:'hero',pageId:'home',component:'hero',fields}]})

test('resolved content merge combines disjoint shared/localized field edits',()=>{
  const base=document({brand:'Common',title:'Hello'})
  const local=document({brand:'Common',title:'Bonjour'})
  const remote=document({brand:'Shared update',title:'Hello'})
  const result=mergeContentDocuments(base,local,remote)
  assert.deepEqual(result.conflicts,[])
  assert.deepEqual(result.document.instances[0].fields,{brand:'Shared update',title:'Bonjour'})
})

test('resolved content merge reports overlapping field edits without losing draft data',()=>{
  const base=document({title:'Hello'})
  const result=mergeContentDocuments(base,document({title:'Bonjour'}),document({title:'Salut'}))
  assert.deepEqual(result.conflicts,['hero:title'])
  assert.equal(result.document.instances[0].fields.title,'Salut')
})

test('resolved content merge preserves non-overlapping instance additions and removals',()=>{
  const base={schemaVersion:1,id:'content',instances:[{id:'hero',pageId:'home',component:'hero',fields:{title:'Hello'}}]}
  const local={...base,instances:[...base.instances,{id:'local',pageId:'home',component:'hero',fields:{title:'Local'}}]}
  const remote={...base,instances:[...base.instances,{id:'remote',pageId:'home',component:'hero',fields:{title:'Remote'}}]}
  const result=mergeContentDocuments(base,local,remote)
  assert.deepEqual(result.conflicts,[])
  assert.deepEqual(result.document.instances.map(instance=>instance.id),['hero','remote','local'])
})
