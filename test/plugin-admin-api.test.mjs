import test from 'node:test'
import assert from 'node:assert/strict'
import {listGatewayPlugins,loadPluginAdminSurface,loadPluginAdminOptions,queryPluginAdmin,runPluginAdminAction,PluginSurfaceChangedError} from '../src/api.ts'
import {installConstructorBridge} from '../src/bridge.ts'

const validSurface=()=>({
  version:1,plugin:'forms-db',manifestVersion:'1.4.0',surfaceDigest:'a'.repeat(64),
  requiredCapabilities:['admin.surface.get','forms.list'],
  pages:[{id:'submissions',title:'Submissions',capability:'forms.list',sections:[{id:'records',kind:'table',dataCapability:'forms.list',columns:['id']}]}],
})

test('Gateway plugin list is strict and the UI selects only typed instances',async()=>{
  const restore=installConstructorBridge({request:async input=>{
    assert.equal(String(input),'/api/plugins')
    return new Response(JSON.stringify({items:[{id:'forms-db',state:'ready',capabilities:['admin.surface.get'],limits:{},health:true}]}))
  }})
  try{
    assert.deepEqual(await listGatewayPlugins(),[{id:'forms-db',state:'ready',capabilities:['admin.surface.get'],limits:{},health:true}])
  }finally{restore()}
})

test('Admin Surface load validates instance, schema and ETag against digest',async()=>{
  const restore=installConstructorBridge({request:async input=>{
    assert.equal(String(input),'/api/plugins/forms-db/admin/surface')
    return new Response(JSON.stringify(validSurface()),{headers:{'Content-Type':'application/json',ETag:`"${'a'.repeat(64)}"`}})
  }})
  try{assert.equal((await loadPluginAdminSurface('forms-db')).pages[0].id,'submissions')}finally{restore()}
  const badETag=installConstructorBridge({request:async()=>new Response(JSON.stringify(validSurface()),{headers:{ETag:`"${'b'.repeat(64)}"`}})})
  try{await assert.rejects(loadPluginAdminSurface('forms-db'),/ETag does not match/)}finally{badETag()}
})

test('Admin query binds its exact page and surface digest; stale surface is typed',async()=>{
  const requests=[]
  const restore=installConstructorBridge({request:async(input,init)=>{
    requests.push({url:String(input),init})
    return new Response(JSON.stringify({rows:[{id:'one'}]}))
  }})
  try{
    assert.deepEqual(await queryPluginAdmin('forms-db','submissions','a'.repeat(64),{mode:'data',input:{site:'main'}}),{rows:[{id:'one'}]})
    assert.equal(requests[0].url,'/api/plugins/forms-db/admin/pages/submissions/query')
    assert.equal(requests[0].init.headers['If-Match'],`"${'a'.repeat(64)}"`)
    assert.deepEqual(JSON.parse(requests[0].init.body),{mode:'data',input:{site:'main'}})
    assert.equal('Authorization' in requests[0].init.headers,false)
  }finally{restore()}
  const stale=installConstructorBridge({request:async()=>new Response(JSON.stringify({code:'plugin_surface_changed'}),{status:409})})
  try{await assert.rejects(queryPluginAdmin('forms-db','submissions','a'.repeat(64),{}),PluginSurfaceChangedError)}finally{stale()}
})

test('dynamic options use only their declared capability/input and validate typed results',async()=>{
  const requests=[]
  const field={key:'site',type:'select',optionsSource:{capability:'forms.list',inputSchema:{type:'object',properties:{region:{type:'string'}},required:['region'],additionalProperties:false},valueField:'id',labelField:'name'}}
  const restore=installConstructorBridge({request:async(input,init)=>{
    requests.push({url:String(input),init})
    return new Response(JSON.stringify({items:[{id:'main',name:'Main site'}]}))
  }})
  try{
    assert.deepEqual(await loadPluginAdminOptions('forms-db','submissions','a'.repeat(64),field,{region:'eu'}),[{value:'main',label:'Main site'}])
    assert.equal(requests[0].url,'/api/plugins/forms-db/admin/pages/submissions/query')
    assert.deepEqual(JSON.parse(requests[0].init.body),{mode:'options',field:'site',input:{region:'eu'}})
    await assert.rejects(loadPluginAdminOptions('forms-db','submissions','a'.repeat(64),field,{}),/region is required/)
  }finally{restore()}
  const malformed=installConstructorBridge({request:async()=>new Response(JSON.stringify({items:[{id:{bad:true},name:'Bad'}]}))})
  try{await assert.rejects(loadPluginAdminOptions('forms-db','submissions','a'.repeat(64),field,{region:'eu'}),/invalid option/)}finally{malformed()}
})

test('dangerous action performs non-dispatching challenge then same-input confirmation retry',async()=>{
  const requests=[];let prompt=''
  const restore=installConstructorBridge({request:async(input,init)=>{
    requests.push({url:String(input),init})
    if(requests.length===1)return new Response(JSON.stringify({code:'confirmation_required',confirmationToken:'opaque-token',expiresAt:new Date(Date.now()+60_000).toISOString()}),{status:428})
    return new Response(JSON.stringify({result:'deleted'}))
  }})
  try{
    const result=await runPluginAdminAction('forms-db','submissions','a'.repeat(64),{id:'delete',title:'Delete',capability:'forms.delete',dangerous:true,confirmation:'Delete this record permanently?',inputSchema:{type:'object',properties:{recordId:{type:'string'}},required:['recordId'],additionalProperties:false}},{recordId:'record-1'},message=>{prompt=message;return true})
    assert.deepEqual(result,{result:'deleted'})
    assert.equal(prompt,'Delete this record permanently?')
    assert.equal(requests.length,2)
    assert.equal(requests[0].url,'/api/plugins/forms-db/admin/pages/submissions/actions/delete')
    assert.equal(requests[0].init.headers['Idempotency-Key'],requests[1].init.headers['Idempotency-Key'])
    assert.equal(requests[1].init.headers['X-Admin-Confirmation'],'opaque-token')
    assert.equal(requests[0].init.body,requests[1].init.body)
  }finally{restore()}
})

test('dangerous action cancellation never sends the confirmation token',async()=>{
  let requests=0
  const restore=installConstructorBridge({request:async()=>{requests++;return new Response(JSON.stringify({code:'confirmation_required',confirmationToken:'opaque-token',expiresAt:new Date(Date.now()+60_000).toISOString()}),{status:428})}})
  try{
    assert.deepEqual(await runPluginAdminAction('forms-db','submissions','a'.repeat(64),{id:'delete',title:'Delete',capability:'forms.delete',dangerous:true,confirmation:'Confirm?',inputSchema:{type:'object',properties:{recordId:{type:'string'}},required:['recordId'],additionalProperties:false}},{recordId:'record-1'},()=>false),{cancelled:true})
    assert.equal(requests,1)
  }finally{restore()}
})

test('dangerous action refuses to retry unless Gateway returns a non-dispatching challenge',async()=>{
  let requests=0
  const restore=installConstructorBridge({request:async()=>{requests++;return new Response(JSON.stringify({result:'unexpected'}))}})
  try{
    await assert.rejects(runPluginAdminAction('forms-db','submissions','a'.repeat(64),{id:'delete',title:'Delete',capability:'forms.delete',dangerous:true,confirmation:'Confirm?',inputSchema:{type:'object',properties:{recordId:{type:'string'}},required:['recordId'],additionalProperties:false}},{recordId:'record-1'},()=>true),/violated the dangerous-action confirmation contract/)
    assert.equal(requests,1)
  }finally{restore()}
})
