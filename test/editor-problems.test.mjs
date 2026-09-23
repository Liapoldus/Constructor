import test from 'node:test'
import assert from 'node:assert/strict'
import {sortProblemsBySeverity} from '../src/editor-problems.ts'

test('problems sort errors before warnings while preserving order within severity',()=>{
  const problems=[
    {id:'warning-1',severity:'warning'},
    {id:'error-1',severity:'error'},
    {id:'info-1',severity:'info'},
    {id:'error-2',severity:'error'},
    {id:'warning-2',severity:'warning'},
  ]
  assert.deepEqual(sortProblemsBySeverity(problems).map(problem=>problem.id),[
    'error-1','error-2','warning-1','warning-2','info-1',
  ])
})

test('problem sorting is immutable and keeps unknown severities last',()=>{
  const problems=[{id:'other',severity:'critical'},{id:'warning',severity:'warning'}]
  const sorted=sortProblemsBySeverity(problems)
  assert.deepEqual(sorted.map(problem=>problem.id),['warning','other'])
  assert.deepEqual(problems.map(problem=>problem.id),['other','warning'])
})

test('identical diagnostics are shown once and keep their first position',()=>{
  const duplicate={id:'same',code:'request.failed',severity:'error',message:'not found'}
  const result=sortProblemsBySeverity([duplicate,{id:'other',severity:'warning'},duplicate])
  assert.deepEqual(result.map(problem=>problem.id),['same','other'])
})
