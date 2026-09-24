import assert from 'node:assert/strict'
import test from 'node:test'
import {caddyfileDraftForProject,createCaddyfileDraft,createCaddyfileDraftCache,editCaddyfileDraft,finishCaddyfileSave,startCaddyfileSave} from '../src/caddyfile-draft.ts'
import {CaddyfileSaveError,loadCaddyfile,saveCaddyfile} from '../src/api.ts'
import {installConstructorBridge} from '../src/bridge.ts'
import React from 'react'
import {renderToStaticMarkup} from 'react-dom/server'
import {CaddyfileEditorView} from '../src/caddyfile-editor-view.tsx'

test('native Caddyfile draft preserves exact source text and tracks dirty state',()=>{
  const source='example.com {\n\trespond "hello"\n}\n'
  const loaded=createCaddyfileDraft(source,'revision-1')
  assert.equal(loaded.text,source)
  assert.equal(editCaddyfileDraft(loaded,source).status,'saved')
  const edited=editCaddyfileDraft(loaded,source+'\n# note\n')
  assert.equal(edited.text,source+'\n# note\n')
  assert.equal(edited.status,'dirty')
})

test('unsaved native Caddyfile drafts remain isolated per project during project switches',()=>{
  const cache=createCaddyfileDraftCache()
  const one=editCaddyfileDraft(createCaddyfileDraft('one {}','one-rev'),'one {\n respond 200\n}')
  const two=createCaddyfileDraft('two {}','two-rev')
  cache.set('project-one',one)
  cache.set('project-two',two)
  assert.equal(cache.get('project-one').text,'one {\n respond 200\n}')
  assert.equal(cache.get('project-one').status,'dirty')
  assert.equal(cache.get('project-two').text,'two {}')
})

test('editor hides and disables the prior project draft during a switch until the new project loads',()=>{
  const prior=createCaddyfileDraft('old-project-secret-source','old-revision')
  const visible=caddyfileDraftForProject('new-project','old-project',prior)
  const markup=renderToStaticMarkup(React.createElement(CaddyfileEditorView,{draft:visible,disabled:true,onChange:()=>{},onSave:()=>{}}))
  assert.equal(visible.status,'loading')
  assert.doesNotMatch(markup,/old-project-secret-source/)
  assert.match(markup,/<textarea[^>]*disabled/)
  assert.match(markup,/<button[^>]*disabled/)
})

test('Caddyfile API reads and writes the exact plain text with optimistic revision',async()=>{
  const source='example.com {\r\n\trespond "hello"\r\n}\r\n'
  const requests=[]
  const restore=installConstructorBridge({request:async(url,init={})=>{
    requests.push({url:String(url),method:init.method??'GET',headers:init.headers,body:init.body})
    return init.method==='PUT'?new Response(JSON.stringify({path:'Caddyfile',revision:'revision-2'}),{headers:{ETag:'revision-2'}}):new Response(source,{headers:{ETag:'revision-1'}})
  }})
  try{
    assert.deepEqual(await loadCaddyfile('project-one'),{text:source,revision:'revision-1',diagnostics:[]})
    assert.deepEqual(await saveCaddyfile('project-one',source,'revision-1'),{text:source,revision:'revision-2',diagnostics:[]})
    assert.equal(requests[0].url,'/api/v1/projects/project-one/files/Caddyfile')
    assert.equal(requests[1].url,'/api/v1/projects/project-one/files/Caddyfile')
    assert.equal(requests[1].headers['If-Match'],'revision-1')
    assert.equal(requests[1].headers['Content-Type'],'text/plain; charset=utf-8')
    assert.equal(requests[1].body,source)
  }finally{restore()}
})

test('Caddyfile API scopes operations to an escaped project ID',async()=>{
  const requests=[]
  const restore=installConstructorBridge({request:async(url)=>{requests.push(String(url));return new Response('source',{headers:{ETag:'r1'}})}})
  try{
    await loadCaddyfile('project/with space')
    assert.deepEqual(requests,['/api/v1/projects/project%2Fwith%20space/files/Caddyfile'])
  }finally{restore()}
})

test('Caddyfile load failures do not expose plain or malformed backend bodies',async()=>{
  for(const body of ['secret source from backend','{"detail": "unterminated secret source']){
    const restore=installConstructorBridge({request:async()=>new Response(body,{status:502})})
    try{await assert.rejects(loadCaddyfile('project-one'),error=>{assert.doesNotMatch(error.message,/secret source/);assert.match(error.message,/502/);return true})}finally{restore()}
  }
})

test('Caddyfile save errors expose only diagnostics returned by the backend',async()=>{
  const diagnostics=[{code:'caddy.adapt.invalid',severity:'error',message:'adapter rejected source',path:'Caddyfile'},{code:'bad code','severity':'secret','message':'not a valid diagnostic'}]
  const restore=installConstructorBridge({request:async()=>new Response(JSON.stringify({detail:'adaptation failed',diagnostics}),{status:422})})
  try{
    await assert.rejects(saveCaddyfile('project-one','invalid {','revision-1'),error=>{
      assert.ok(error instanceof CaddyfileSaveError)
      assert.equal(error.message,'adaptation failed')
      assert.equal(error.conflict,false)
      assert.deepEqual(error.diagnostics,[diagnostics[0]])
      return true
    })
  }finally{restore()}
})

test('Caddyfile save errors never expose unstructured response bodies',async()=>{
  const restore=installConstructorBridge({request:async()=>new Response('source contains private configuration token=do-not-show',{status:500})})
  try{
    await assert.rejects(saveCaddyfile('project-one','draft','revision-1'),error=>{
      assert.ok(error instanceof CaddyfileSaveError)
      assert.doesNotMatch(error.message,/private configuration|do-not-show/)
      assert.match(error.message,/500/)
      return true
    })
  }finally{restore()}
})

test('Caddyfile editor UI renders raw draft and only supplied backend diagnostics',()=>{
  const source='example.com {\n  respond "<hello>"\n}\n'
  const base={text:source,savedText:'',revision:'rev-1',status:'dirty',diagnostics:[]}
  const emptyMarkup=renderToStaticMarkup(React.createElement(CaddyfileEditorView,{draft:base,disabled:false,onChange:()=>{},onSave:()=>{}}))
  assert.match(emptyMarkup,/aria-label="Caddyfile source"/)
  assert.match(emptyMarkup,/example\.com \{[\s\S]*respond &quot;&lt;hello&gt;&quot;/)
  assert.doesNotMatch(emptyMarkup,/diagnostic/)
  const withBackendDiagnostic={...base,diagnostics:[{code:'caddy.adapt.invalid',severity:'error',message:'Backend rejected the Caddyfile.'}]}
  const errorMarkup=renderToStaticMarkup(React.createElement(CaddyfileEditorView,{draft:withBackendDiagnostic,disabled:false,onChange:()=>{},onSave:()=>{}}))
  assert.match(errorMarkup,/caddy\.adapt\.invalid/)
  assert.match(errorMarkup,/Backend rejected the Caddyfile\./)
})

test('save completion retains edits made while saving and only uses backend diagnostics',()=>{
  const source='example.com {\n respond 200\n}\n'
  const started=startCaddyfileSave(editCaddyfileDraft(createCaddyfileDraft(source,'revision-1'),source+'# pending\n'))
  assert.equal(started.status,'saving')
  const changedDuringSave=editCaddyfileDraft(started,source+'# newer draft\n')
  const diagnostics=[{code:'caddy.adapt.invalid',severity:'error',message:'backend diagnostic'}]
  const completed=finishCaddyfileSave(changedDuringSave,source,'revision-2',diagnostics)
  assert.equal(completed.text,source+'# newer draft\n')
  assert.equal(completed.savedText,source)
  assert.equal(completed.status,'dirty')
  assert.deepEqual(completed.diagnostics,diagnostics)
})
