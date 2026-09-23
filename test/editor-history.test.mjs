import assert from 'node:assert/strict'
import test from 'node:test'
import * as history from '../src/editor-history.ts'

test('coalesces consecutive edits to the same field',()=>{
  const initial={title:'Initial'}
  const selection={selected:'Hero',selectedPageID:'home',selectedInstanceID:'hero'}
  const first=history.recordHistory([],initial,selection,'hero:title',1000)
  const second=history.recordHistory(first,{title:'H'},{...selection},'hero:title',1200)
  const third=history.recordHistory(second,{title:'Hero'},{...selection},'hero:title',1500)
  assert.equal(third.length,1)
  assert.deepEqual(third[0].document,initial)
})

test('undo and redo restore both the document and selection',()=>{
  const before={instances:['hero']}
  const after={instances:['hero','button']}
  const beforeSelection={selected:'Hero',selectedPageID:'home',selectedInstanceID:'hero'}
  const afterSelection={selected:'Button',selectedPageID:'home',selectedInstanceID:'button'}
  const past=history.recordHistory([],before,beforeSelection,undefined,1000)
  const undone=history.undoHistory(after,afterSelection,past,[])
  assert.deepEqual(undone.document,before)
  assert.deepEqual(undone.selection,beforeSelection)
  const redone=history.redoHistory(undone.document,undone.selection,undone.past,undone.future)
  assert.deepEqual(redone.document,after)
  assert.deepEqual(redone.selection,afterSelection)
})

test('history is bounded and undo at its beginning is a no-op',()=>{
  let past=[]
  for(let index=0;index<105;index++)past=history.recordHistory(past,index,{selected:String(index)},undefined,index*1000)
  assert.equal(past.length,100)
  assert.equal(history.undoHistory(105,{selected:'105'},[],[]),null)
})
