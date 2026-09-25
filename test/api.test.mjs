import test from 'node:test'; import assert from 'node:assert/strict'
import {apiErrorMessage,checkoutGitBranch,commitProject,createGitBranch,createProjectPage,createProjectSite,deploySnapshot,loadGitBranches,loadGitDiff,loadGitHistory,loadGitStatus,rollbackDeployment,saveProjectContent,saveProjectTheme,saveSiteDocument,toRuntimeContent,uploadProjectAsset,validateProject,generateLocale,loadProject,listProjectAssets,listProjectThemes,listProjects,listGatewayGroups,listGatewayGroupReleases} from '../src/api.ts'
import {installConstructorBridge} from '../src/bridge.ts'
test('API contract is versioned',()=>assert.equal('/api/v1/project'.startsWith('/api/v1/'),true))
test('project list rejects duplicate stable IDs instead of rendering ambiguous options',async()=>{
  const restore=installConstructorBridge({request:async()=>new Response(JSON.stringify({projects:[{id:'demo',name:'Demo'},{id:'demo',name:'Duplicate'}]}))})
  try{await assert.rejects(listProjects(),/duplicate project id: demo/)}finally{restore()}
})
test('all UI API calls can use the injected desktop bridge',async()=>{
  const requests=[]
  const restore=installConstructorBridge({request:async(input,init)=>{
    requests.push({input:String(input),method:init?.method??'GET'})
    return new Response(JSON.stringify({valid:true,diagnostics:[]}))
  }})
  try {
    assert.deepEqual(await validateProject(),{valid:true,diagnostics:[]})
    assert.deepEqual(requests,[{input:'/api/v1/project/validate',method:'POST'}])
  } finally {restore()}
})

test('Gateway group and revision views use Constructor read proxies, not legacy site endpoints',async()=>{
  const requests=[]
  const restore=installConstructorBridge({request:async(url)=>{
    requests.push(String(url))
    if(String(url)==='/api/v1/gateway/groups')return new Response(JSON.stringify({items:[{id:'frontend',kind:'application',active:true,currentRevision:'a'.repeat(64),previousRevision:null,state:'ready'}],requestId:'groups-request'}))
    if(String(url)==='/api/v1/gateway/groups/frontend/releases?limit=25')return new Response(JSON.stringify({items:[{id:'a'.repeat(64),groupId:'frontend',caddyfileDigest:'b'.repeat(64),artifactDigest:null,createdAt:'2026-09-25T10:00:00Z',actor:'operator'}],nextCursor:null,requestId:'releases-request'}))
    throw new Error(`unexpected request: ${String(url)}`)
  }})
  try{
    const groups=await listGatewayGroups()
    const revisions=await listGatewayGroupReleases('frontend',{limit:25})
    assert.equal(groups.items[0].id,'frontend')
    assert.equal(revisions.items[0].id,'a'.repeat(64))
    assert.deepEqual(requests,['/api/v1/gateway/groups','/api/v1/gateway/groups/frontend/releases?limit=25'])
    assert.equal(requests.some(path=>path.includes('/api/sites')),false)
  }finally{restore()}
})
test('page creation sends the selected Site and optimistic revisions',async()=>{
  const originalFetch=globalThis.fetch;let body
  globalThis.fetch=async(url,options)=>{
    assert.equal(String(url),'/api/v1/project/pages')
    assert.equal(options.method,'POST')
    body=JSON.parse(options.body)
    return new Response(JSON.stringify({page:{id:'about',name:'About us'}}),{status:201})
  }
  try {
    const request={siteId:'site-a',id:'about',name:'About us',routePath:'/about',revisions:{manifest:'m1',site:'s1',routes:'r1'}}
    assert.deepEqual(await createProjectPage(request),{page:{id:'about',name:'About us'}})
    assert.deepEqual(body,request)
  } finally {globalThis.fetch=originalFetch}
})

test('Site creation sends the selected source and optimistic revisions',async()=>{
  let request
  const restore=installConstructorBridge({request:async(url,init={})=>{
      request={url:String(url),body:JSON.parse(init.body)}
      return new Response(JSON.stringify({site:{id:'campaign',name:'Campaign',pages:[{id:'home',name:'Home'}],locales:['en-US']}}),{status:201})
    }})
  try{
    const payload={id:'campaign',name:'Campaign',sourceSiteId:'main',revisions:{manifest:'manifest-etag',sourceSite:'site-etag'}}
    assert.equal((await createProjectSite(payload)).site.id,'campaign')
    assert.equal(request.url,'/api/v1/project/sites')
    assert.deepEqual(request.body,payload)
  }finally{restore()}
})
test('asset picker API returns the verified active-project catalog',async()=>{
  const restore=installConstructorBridge({request:async(url)=>{
    assert.equal(String(url),'/api/v1/project/assets')
    return new Response(JSON.stringify({assets:[{id:'logo-image',type:'image',path:'public/assets/logo.png',mimeType:'image/png',size:12,sha256:'a'.repeat(64)}]}))
  }})
  try{assert.deepEqual(await listProjectAssets(),[{id:'logo-image',type:'image',path:'public/assets/logo.png',mimeType:'image/png',size:12,sha256:'a'.repeat(64)}])}finally{restore()}
})
test('theme list API returns validated typed project themes',async()=>{
  const themes=[{schemaVersion:1,id:'brand',name:'Brand',tokens:{'colors.primary':{type:'color',value:'#123456'}}}]
  const restore=installConstructorBridge({request:async(url)=>{
    assert.equal(String(url),'/api/v1/project/themes')
    return new Response(JSON.stringify({themes}))
  }})
  try{assert.deepEqual(await listProjectThemes(),themes)}finally{restore()}
})
test('theme edits use the document revision and do not persist the transport ETag',async()=>{
  const theme={schemaVersion:1,id:'brand',name:'Brand',tokens:{'colors.primary':{type:'color',value:'#123456'}},revision:'theme-etag'}
  let request
  const restore=installConstructorBridge({request:async(url,init)=>{
    request={url:String(url),headers:init.headers,body:JSON.parse(init.body)}
    return new Response(JSON.stringify({path:'liapoldus/themes/brand.json',revision:'next-etag'}),{headers:{ETag:'next-etag'}})
  }})
  try{
    assert.equal(await saveProjectTheme(theme,theme.revision),'next-etag')
    assert.equal(request.url,'/api/v1/project/file?path=liapoldus%2Fthemes%2Fbrand.json')
    assert.equal(request.headers['If-Match'],'theme-etag')
    assert.equal('revision' in request.body,false)
  }finally{restore()}
})
test('asset upload sends the file bytes through the Constructor bridge',async()=>{
  let request
  const asset={id:'asset-123456789012345678901234',type:'image',path:'public/assets/asset-123456789012345678901234.png',mimeType:'image/png',size:3,sha256:'a'.repeat(64)}
  const restore=installConstructorBridge({request:async(url,init)=>{
    request={url:String(url),method:init.method,body:init.body}
    return new Response(JSON.stringify({asset,reused:false}),{status:201})
  }})
  try{
    const file=new File([new Uint8Array([1,2,3])],'pixel.png',{type:'image/png'})
    assert.deepEqual(await uploadProjectAsset(file),{asset,reused:false})
    assert.equal(request.url,'/api/v1/project/assets')
    assert.equal(request.method,'POST')
    assert.deepEqual([...new Uint8Array(await request.body.arrayBuffer())],[1,2,3])
    await assert.rejects(uploadProjectAsset(new File([], 'empty.png')),/non-empty/)
  }finally{restore()}
})
test('Git overview reads the active repository endpoints',async()=>{
  const requests=[]
  const restore=installConstructorBridge({request:async(url)=>{
    requests.push(String(url))
    const payload={
      '/api/v1/git/status':{files:[' M src/app.tsx']},
      '/api/v1/git/diff':{diff:'diff --git a/src/app.tsx b/src/app.tsx'},
      '/api/v1/git/branches':{branches:[{name:'main',commit:'a'.repeat(40),current:true}]},
      '/api/v1/git/history':{commits:[{hash:'a'.repeat(40),author:'Oleg',date:'2026-09-23',message:'Initial'}]},
    }[String(url)]
    return new Response(JSON.stringify(payload))
  }})
  try{
    assert.deepEqual(await loadGitStatus(),[' M src/app.tsx'])
    assert.match(await loadGitDiff(),/^diff --git/)
    assert.equal((await loadGitBranches())[0].current,true)
    assert.equal((await loadGitHistory())[0].message,'Initial')
    assert.deepEqual(requests,['/api/v1/git/status','/api/v1/git/diff','/api/v1/git/branches','/api/v1/git/history'])
  }finally{restore()}
})
test('Git commit is explicit and sends the requested message',async()=>{
  let request
  const restore=installConstructorBridge({request:async(url,init)=>{
    request={url:String(url),method:init.method,body:JSON.parse(init.body)}
    return new Response(JSON.stringify({revision:'b'.repeat(40)}),{status:201})
  }})
  try{
    assert.equal(await commitProject('feat: editor'),'b'.repeat(40))
    assert.deepEqual(request,{url:'/api/v1/git/commit',method:'POST',body:{message:'feat: editor'}})
  }finally{restore()}
})
test('local Git branch mutations use typed API boundary and preserve branch names',async()=>{
  const requests=[]
  const restore=installConstructorBridge({request:async(url,init)=>{
    requests.push({url:String(url),method:init.method,body:JSON.parse(init.body)})
    return new Response(JSON.stringify({branch:{name:'feature/editor',commit:'c'.repeat(40),current:true}}),{status:201})
  }})
  try{
    assert.equal((await createGitBranch('feature/editor')).current,true)
    assert.equal((await checkoutGitBranch('feature/editor')).name,'feature/editor')
    assert.deepEqual(requests,[
      {url:'/api/v1/git/branches',method:'POST',body:{name:'feature/editor'}},
      {url:'/api/v1/git/checkout',method:'POST',body:{name:'feature/editor'}},
    ])
    await assert.rejects(createGitBranch(' '),/Branch name is required/)
  }finally{restore()}
})
test('publish and rollback requests bind idempotency and confirmation to exact targets',async()=>{
  const requests=[]
  const restore=installConstructorBridge({request:async(url,init={})=>{
    requests.push({url:String(url),body:init.body?JSON.parse(init.body):undefined})
    return new Response(JSON.stringify({id:'deployment-1',status:'active'}),{status:201})
  }})
  try{
    await deploySnapshot('snapshot-1','build-1','site-a','production')
    await deploySnapshot('snapshot-2','build-2','site-a','production','a'.repeat(64))
    await rollbackDeployment('deployment-1','site-a','production')
    assert.deepEqual(requests[0],{url:'/api/v1/deployments',body:{id:'deployment-snapshot-1-production',siteId:'site-a',environmentId:'production',snapshotId:'snapshot-1',buildId:'build-1',confirmedTarget:'site-a/production'}})
    assert.equal(requests[1].body.confirmedGatewayRevision,'a'.repeat(64))
    assert.equal(requests[2].url,'/api/v1/deployments/deployment-1/rollback?confirmedTarget=site-a%2Fproduction')
    assert.throws(()=>deploySnapshot('snapshot-1','build-1','','production'),/Site and environment are required/)
    assert.throws(()=>rollbackDeployment('deployment-1','','production'),/Site and environment are required/)
  }finally{restore()}
})
test('Site page metadata writes preserve stable IDs and use If-Match',async()=>{
  const originalFetch=globalThis.fetch;let request
  globalThis.fetch=async(url,options)=>{
    request={url:String(url),options}
    return new Response(JSON.stringify({path:'liapoldus/sites/site-a.json'}),{status:200,headers:{ETag:'site-rev-2'}})
  }
  try {
    const document={schemaVersion:1,id:'site-a',projectId:'demo',name:'Site A',pages:[{id:'about',name:'About'},{id:'home',name:'Homepage'}],locales:['en-US']}
    assert.equal(await saveSiteDocument(document,'site-rev-1'),'site-rev-2')
    assert.equal(request.url,'/api/v1/project/file?path=liapoldus%2Fsites%2Fsite-a.json')
    assert.equal(request.options.headers['If-Match'],'site-rev-1')
    assert.deepEqual(JSON.parse(request.options.body),document)
  } finally {globalThis.fetch=originalFetch}
})
test('API errors expose RFC 9457 detail and validation diagnostics cleanly',async()=>{
  const problem=new Response(JSON.stringify({type:'about:blank',title:'Unprocessable Entity',status:422,code:'project_validation_failed',detail:'Validation failed',diagnostics:[{code:'content.required',message:'title is required'}]}),{status:422,headers:{'Content-Type':'application/problem+json'}})
  assert.equal(await apiErrorMessage(problem,'fallback'),'content.required: title is required')
  const conflict=new Response(JSON.stringify({type:'about:blank',title:'Conflict',status:409,code:'revision_conflict',detail:'The document changed since the supplied revision.',error:'project file conflict'}),{status:409})
  assert.equal(await apiErrorMessage(conflict,'fallback'),'The document changed since the supplied revision.')
})
test('runtime content keeps all instance IDs, pages, and fields',()=>{
  const document={schemaVersion:1,id:'fixture',instances:[
    {id:'hero-1',pageId:'home',component:'hero',fields:{title:'Home title'}},
    {id:'hero-2',pageId:'home',component:'hero',fields:{title:'Second hero'}},
    {id:'cta-1',pageId:'about',component:'button',fields:{label:'Contact'}}
  ]}
  assert.deepEqual(toRuntimeContent(document),{pages:{
    home:{instances:{'hero-1':{component:'hero',fields:{title:'Home title'}},'hero-2':{component:'hero',fields:{title:'Second hero'}}}},
    about:{instances:{'cta-1':{component:'button',fields:{label:'Contact'}}}}
  }})
})
test('validation without Site/Locale context uses the project-wide endpoint',async()=>{
  const originalFetch=globalThis.fetch;let requested=''
  globalThis.fetch=async(url,options)=>{requested=String(url);assert.equal(options.method,'POST');return new Response(JSON.stringify({valid:true,diagnostics:[]}))}
  try {
    assert.deepEqual(await validateProject(),{valid:true,diagnostics:[]})
    assert.equal(requested,'/api/v1/project/validate')
    await assert.rejects(validateProject('site-a'),/provided together/)
    await assert.rejects(generateLocale(),/Site and locale are required/)
  } finally {globalThis.fetch=originalFetch}
})
test('project open selects an enabled Site locale instead of assuming ru-RU',async()=>{
  const originalFetch=globalThis.fetch;const requested=[]
  globalThis.fetch=async(url)=>{
    const value=String(url);requested.push(value)
    if(value==='/api/v1/project')return new Response(JSON.stringify({id:'demo',name:'Demo',pages:['home','contact'],components:[],react:{entry:'src/app.tsx'}}))
    const path=new URLSearchParams(value.split('?')[1]).get('path')
    if(path==='liapoldus/sites/site-a.json')return new Response(JSON.stringify({schemaVersion:1,id:'site-a',projectId:'demo',name:'Site A',pages:[{id:'home',name:'Accueil'},{id:'contact',name:'Accueil'}],locales:['fr-FR']}))
    if(value.startsWith('/api/v1/project/content?'))return new Response(JSON.stringify({document:{schemaVersion:1,id:'fr-content',instances:[]},contentRevision:'fr-revision',defaultContentRevision:'default-revision'}))
    throw new Error(`unexpected request ${value}`)
  }
  try {
    const project=await loadProject('site-a')
    assert.equal(project.locale,'fr-FR')
    assert.equal(project.siteId,'site-a')
    assert.deepEqual(project.pageIDs,['home','contact'])
    assert.deepEqual(project.sitePages,{home:'Accueil',contact:'Accueil'})
    assert.deepEqual(project.siteDocument.pages,[{id:'home',name:'Accueil'},{id:'contact',name:'Accueil'}])
    assert.equal('pages' in project,false)
    assert.ok(requested.every(url=>!url.includes('local-site')&&!url.includes('ru-RU')))
  } finally {globalThis.fetch=originalFetch}
})
test('project open identifies document-level language/default fallback without borrowing its revision',async()=>{
  const originalFetch=globalThis.fetch;const requested=[]
  const fallback={schemaVersion:1,id:'site-a-default-content',instances:[]}
  globalThis.fetch=async url=>{
    const value=String(url);requested.push(value)
    if(value==='/api/v1/project')return new Response(JSON.stringify({id:'demo',name:'Demo',pages:['home'],components:[],react:{entry:'src/app.tsx'}}))
    const path=new URLSearchParams(value.split('?')[1]).get('path')
    if(path==='liapoldus/sites/site-a.json')return new Response(JSON.stringify({schemaVersion:1,id:'site-a',projectId:'demo',name:'Site A',pages:['home'],locales:['fr-FR']}))
    if(value.startsWith('/api/v1/project/content?'))return new Response(JSON.stringify({document:fallback,defaultContentRevision:'default-revision',fallbackFrom:'default'}))
    throw new Error(`unexpected request ${value}`)
  }
  try {
    const project=await loadProject('site-a')
    assert.deepEqual(project.contentDocument,fallback)
    assert.equal(project.contentFallbackFrom,'default')
    assert.equal(project.contentRevision,undefined,'fallback revision must never be used to save the selected locale')
    assert.ok(requested.some(url=>url==='/api/v1/project/content?siteId=site-a&locale=fr-FR'))
  } finally {globalThis.fetch=originalFetch}
})
test('default is an editing scope distinct from enabled Site locales',async()=>{
  const originalFetch=globalThis.fetch
  globalThis.fetch=async url=>{
    const value=String(url)
    if(value==='/api/v1/project')return new Response(JSON.stringify({id:'demo',name:'Demo',pages:['home'],components:[],react:{entry:'src/app.tsx'}}))
    const path=new URLSearchParams(value.split('?')[1]).get('path')
    if(path==='liapoldus/sites/site-a.json')return new Response(JSON.stringify({schemaVersion:1,id:'site-a',projectId:'demo',name:'Site A',pages:['home'],locales:['fr-FR']}))
    if(value==='/api/v1/project/content?siteId=site-a&locale=default')return new Response(JSON.stringify({document:{schemaVersion:1,id:'default-content',instances:[]},contentRevision:'default-rev',defaultContentRevision:'default-rev',fallbackFrom:'default'}))
    throw new Error(`unexpected request ${value}`)
  }
  try{
    const project=await loadProject('site-a','default')
    assert.equal(project.locale,'default')
    assert.deepEqual(project.locales,['fr-FR'])
    assert.equal(project.contentRevision,'default-rev')
    assert.equal(project.contentFallbackFrom,undefined)
  }finally{globalThis.fetch=originalFetch}
})
test('project opening rejects Site page IDs missing from the manifest',async()=>{
  const originalFetch=globalThis.fetch
  globalThis.fetch=async url=>{
    const value=String(url)
    if(value==='/api/v1/project')return new Response(JSON.stringify({id:'demo',name:'Demo',pages:['home'],components:[],react:{entry:'src/app.tsx'}}))
    const path=new URLSearchParams(value.split('?')[1]).get('path')
    if(path==='liapoldus/sites/site-a.json')return new Response(JSON.stringify({schemaVersion:1,id:'site-a',projectId:'demo',name:'Site A',pages:[{id:'missing',name:'Page'}],locales:['fr-FR']}))
    if(value.startsWith('/api/v1/project/content?'))return new Response(JSON.stringify({document:{schemaVersion:1,id:'fr-content',instances:[]}}))
    throw new Error(`unexpected request ${value}`)
  }
  try {await assert.rejects(loadProject('site-a'),/not declared by the project manifest/)}
  finally {globalThis.fetch=originalFetch}
})
test('localized content save sends resolved base, target and both optimistic revisions',async()=>{
  const originalFetch=globalThis.fetch;let request
  globalThis.fetch=async(url,options)=>{request={url:String(url),options};return new Response(JSON.stringify({contentRevision:'locale-next',defaultContentRevision:'default-next'}))}
  try{
    const document={schemaVersion:1,id:'doc',instances:[]}
    const result=await saveProjectContent(document,document,'site-a','fr-FR','locale-rev','default-rev')
    assert.deepEqual(result,{contentRevision:'locale-next',defaultContentRevision:'default-next'})
    assert.equal(request.url,'/api/v1/project/content?siteId=site-a&locale=fr-FR')
    assert.deepEqual(JSON.parse(request.options.body),{base:document,document,revisions:{locale:'locale-rev',default:'default-rev'}})
  }finally{globalThis.fetch=originalFetch}
})
