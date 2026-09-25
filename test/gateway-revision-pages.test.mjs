import test from 'node:test'; import assert from 'node:assert/strict'
import {appendGatewayRevisionPage,cacheGatewayRevisionView,createGatewayRevisionView,gatewayRevisionViewForGroup,selectGatewayRevisionPage} from '../src/gateway-revision-pages.ts'

const summary=id=>({id:id.repeat(64),groupId:'frontend',caddyfileDigest:'b'.repeat(64),artifactDigest:null,createdAt:'2026-09-25T10:00:00Z'})
const page=(items,nextCursor)=>({items,nextCursor,requestId:'request'})

test('revision pages keep an opaque cursor and can restore the prior page',()=>{
  const first=page([summary('a')],'cursor/+ two')
  const second=page([summary('c')],null)
  const view=createGatewayRevisionView(first)
  const extended=appendGatewayRevisionPage(view,'cursor/+ two',second)
  assert.equal(selectGatewayRevisionPage(extended,0),first)
  assert.equal(selectGatewayRevisionPage(extended,1),second)
  assert.equal(extended.pageIndex,1)
})

test('revision page state is isolated by group and a new group starts at page one',()=>{
  const frontend=createGatewayRevisionView(page([summary('a')],'next'))
  const system=createGatewayRevisionView(page([summary('d')],null))
  const cache=cacheGatewayRevisionView(cacheGatewayRevisionView({},'frontend',frontend),'system',system)
  const savedFrontend=gatewayRevisionViewForGroup(cache,'frontend')
  const savedSystem=gatewayRevisionViewForGroup(cache,'system')
  assert.equal(savedFrontend.pageIndex,0)
  assert.equal(savedSystem.pageIndex,0)
  assert.equal(selectGatewayRevisionPage(savedFrontend,0).items[0].id,summary('a').id)
  assert.equal(selectGatewayRevisionPage(savedSystem,0).items[0].id,summary('d').id)
  assert.equal(gatewayRevisionViewForGroup(cache,'new-group'),undefined)
})

test('revision page append rejects stale, repeated-cursor, and duplicate revisions',()=>{
  const first=page([summary('a')],'cursor-2')
  const view=createGatewayRevisionView(first)
  assert.throws(()=>appendGatewayRevisionPage(view,'stale',page([summary('b')],null)),/cursor does not match/)
  assert.throws(()=>appendGatewayRevisionPage(view,'cursor-2',page([summary('b')],'cursor-2')),/repeated cursor/)
  assert.throws(()=>appendGatewayRevisionPage(view,'cursor-2',page([summary('a')],null)),/duplicate revision/)
})
