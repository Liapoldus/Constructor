import test from 'node:test'
import assert from 'node:assert/strict'
import {previewViewports,previewViewportStyle} from '../src/responsive-preview.ts'

test('responsive preview offers desktop, tablet and mobile viewport widths',()=>{
  assert.deepEqual(previewViewports,{desktop:{label:'Desktop',width:1440},tablet:{label:'Tablet',width:768},mobile:{label:'Mobile',width:390}})
  assert.deepEqual(previewViewportStyle('desktop'),{width:'min(1440px, 100%)',maxWidth:'100%'})
  assert.deepEqual(previewViewportStyle('tablet'),{width:'min(768px, 100%)',maxWidth:'100%'})
  assert.deepEqual(previewViewportStyle('mobile'),{width:'min(390px, 100%)',maxWidth:'100%'})
})
