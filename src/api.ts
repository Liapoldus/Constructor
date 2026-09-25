import {constructorRequest} from './bridge'
import {parseAdminSurface, validateAdminInput, type AdminAction, type AdminField, type AdminSurface} from './plugin-admin-surface'

export type SchemaField = {key:string;type:string;label:string;required?:boolean;nullable?:boolean;default?:unknown;allowedValues?:Array<string|number|boolean>;localized?:boolean}
export type ComponentSchema = {id:string;schemaVersion:number;kind:'component'|'primitive';source:string;fields:SchemaField[];themeTokens?:string[]}
export type ContentInstance = {id:string;pageId:string;component:string;fields:Record<string,unknown>}
export type ContentDocument = {schemaVersion:1;id:string;instances:ContentInstance[]}
export type RuntimeContent = {pages:Record<string,{instances:Record<string,{component:string;fields:Record<string,unknown>}>}>}
export type SitePage = {id:string;name:string}
export type SiteDocument = {schemaVersion:1;id:string;projectId:string;name:string;themeId?:string;pages:(string|SitePage)[];locales:string[]}
export type ThemeTokenType='color'|'fontFamily'|'fontSize'|'fontWeight'|'lineHeight'|'length'|'shadow'|'duration'|'easing'
export type ThemeDocument={schemaVersion:1;id:string;name:string;tokens:Record<string,{type:ThemeTokenType;value:string|number}>;variants?:{light?:Record<string,string|number>;dark?:Record<string,string|number>};revision?:string}
export type Project = {id:string;siteId:string;name: string; pageIDs:string[]; sitePages:Record<string,string>; siteDocument:SiteDocument; components: string[]; locales:string[]; locale:string; contentDocument:ContentDocument; contentRevision?: string; defaultContentRevision?:string; contentFallbackFrom?:string; manifestRevision?:string; siteRevision?:string; schemas: Record<string,ComponentSchema>}
export type ProjectManifest = Pick<Project, 'id'|'name'|'components'> & {pages:string[];react:{entry:string}}
export type ProjectRecord = {id: string; name: string; path: string}
export type Diagnostic = {code: string; severity: string; path: string; message: string; pageId?:string; instanceId?:string; fieldKey?:string}
export type Snapshot = {id: string; siteId: string; gitCommit: string; contentDigest: string; status: string}
export type Build = {id: string; snapshotId: string; status: string; artifactPath?: string; artifactChecksum?: string; error?: string}
export type Deployment = {id: string; siteId: string; environmentId: string; snapshotId: string; buildId: string; status: string; previousId?: string; action?: 'publish'|'rollback'; gatewayRevision?:string}
export type DeploymentTargetState = {siteId:string;environmentId:string;gatewayRevision:string|null;localRevision?:string;requiresBaselineConfirmation:boolean}
export type Environment = {id: string; name: string; kind: string; domain?: string}
export type User = {id: string; name: string; system: boolean}
export type Role = {id: string; name: string; system: boolean}
export type Permission = {key: string}
export type Route = {id:string;path:string;page:string;layout?:string;layouts?:string[];access?:string;chunk?:'same'|'separate'|'lazy'|'preload';lazy?:boolean;preload?:string[];metadata?:Record<string,string>}
export type RouteDocument = {schemaVersion:1;id:string;routes:Route[]}
export type CaddyfileDiagnostic = {code:string;severity:string;message:string;path?:string}
export type CaddyfileSource = {text:string;revision:string;diagnostics:CaddyfileDiagnostic[]}
export type AssetType = 'image'|'icon'|'font'|'video'|'document'|'other'
export type AssetVariantItem = {path:string;mimeType:'image/webp';width:number;height:number;size:number;sha256:string}
export type AssetItem = {id:string;type:AssetType;path:string;mimeType:string;size:number;sha256:string;width?:number;height?:number;variants?:AssetVariantItem[]}
export type AssetUploadResult = {asset:AssetItem;reused:boolean}
export type PreviewSession = {url:string;sessionId:string;active:boolean}
export type PreviewDraftMessage = {protocol:1;source:'liapoldus.constructor';type:'content-draft';sessionId:string;revision:number;content:RuntimeContent;selectedInstanceId?:string|null}
export type GitBranch = {name:string;commit:string;current:boolean}
export type GitCommitRecord = {hash:string;message:string;author:string;date:string}
export type GatewayPluginInstance = {id:string;state:'starting'|'ready'|'unhealthy'|'stopped';capabilities:string[];limits:Record<string,unknown>;health:boolean}
export type GatewayGroup = {id:string;kind:'system'|'application';active:boolean;currentRevision:string|null;previousRevision:string|null;state:'empty'|'ready'}
export type GatewayGroupRevisionSummary = {id:string;groupId:string;caddyfileDigest:string;artifactDigest:string|null;createdAt:string;actor?:string}
export type GatewayGroupRevisionPage = {items:GatewayGroupRevisionSummary[];nextCursor:string|null;requestId:string}
export class PluginSurfaceChangedError extends Error {
  constructor(){super('The plugin Admin Surface changed. Reload its schema before continuing.');this.name='PluginSurfaceChangedError'}
}

export class CaddyfileSaveError extends Error {
  constructor(message:string,readonly conflict:boolean,readonly diagnostics:CaddyfileDiagnostic[]){super(message);this.name='CaddyfileSaveError'}
}

type APIProblem = {detail?:string;error?:string;title?:string;diagnostics?:Diagnostic[]}

export async function apiErrorMessage(response:Response,fallback:string):Promise<string> {
  const body=await response.text()
  try {
    const problem=JSON.parse(body) as APIProblem
    const diagnostics=problem.diagnostics?.map(item=>`${item.code}: ${item.message}`).join('; ')
    return diagnostics||problem.detail||problem.error||problem.title||fallback
  } catch {
    return body||fallback
  }
}

export function toRuntimeContent(document:ContentDocument):RuntimeContent {
  const pages:RuntimeContent['pages']={}
  for(const instance of document.instances){
    const page=pages[instance.pageId]??{instances:{}}
    page.instances[instance.id]={component:instance.component,fields:instance.fields}
    pages[instance.pageId]=page
  }
  return {pages}
}

const gatewayIDPattern=/^[a-z][a-z0-9-]{0,62}$/
const caddyfilePath='Caddyfile'
function readCaddyfileFailure(raw:string,status:number,operation:string):{message:string;diagnostics:CaddyfileDiagnostic[];revision:string} {
  let body:Record<string,unknown>={}
  try{const parsed=JSON.parse(raw);if(parsed&&typeof parsed==='object'&&!Array.isArray(parsed))body=parsed as Record<string,unknown>}catch{/* Ignore non-JSON response bodies. */}
  const diagnostics=Array.isArray(body.diagnostics)?body.diagnostics.filter((item):item is CaddyfileDiagnostic=>{
    if(!item||typeof item!=='object')return false
    const value=item as CaddyfileDiagnostic
    return typeof value.code==='string'&&/^[a-z0-9][a-z0-9._-]{0,127}$/.test(value.code)&&['error','warning','info'].includes(value.severity)&&typeof value.message==='string'&&value.message.length<=512&&!/[\u0000-\u001f\u007f]/.test(value.message)&&(!('path'in value)||typeof value.path==='string'&&value.path.length<=256&&!/[\u0000-\u001f\u007f]/.test(value.path))
  }).slice(0,100):[]
  const structuredError=[body.detail,body.error].find((value):value is string=>typeof value==='string'&&value.trim().length>0&&value.length<=512&&!/[\u0000-\u001f\u007f]/.test(value))
  const revision=typeof body.revision==='string'&&body.revision.length<=256&&!/[\u0000-\u001f\u007f]/.test(body.revision)?body.revision:''
  return {message:structuredError??`${operation} (${status})`,diagnostics,revision}
}
function gatewayID(value:string,label:string):string {
  if(!gatewayIDPattern.test(value))throw new Error(`${label} is invalid`)
  return encodeURIComponent(value)
}
export async function listGatewayPlugins():Promise<GatewayPluginInstance[]> {
  const response=await constructorRequest('/api/plugins')
  if(!response.ok)throw new Error(await apiErrorMessage(response,'Gateway plugins unavailable'))
  const body=await response.json() as {items?:unknown}
  if(!Array.isArray(body.items))throw new Error('Gateway returned an invalid plugin list')
  const items=body.items.map((item):GatewayPluginInstance=>{
    if(!item||typeof item!=='object')throw new Error('Gateway returned an invalid plugin record')
    const value=item as Record<string,unknown>
    if(typeof value.id!=='string'||!gatewayIDPattern.test(value.id)||!['starting','ready','unhealthy','stopped'].includes(String(value.state))||!Array.isArray(value.capabilities)||!value.capabilities.every(capability=>typeof capability==='string')||typeof value.health!=='boolean'||!value.limits||typeof value.limits!=='object'||Array.isArray(value.limits))throw new Error('Gateway returned an invalid plugin record')
    return {id:value.id,state:value.state as GatewayPluginInstance['state'],capabilities:value.capabilities as string[],health:value.health,limits:value.limits as Record<string,unknown>}
  })
  if(new Set(items.map(item=>item.id)).size!==items.length)throw new Error('Gateway returned duplicate plugin instance IDs')
  return items
}
export async function listGatewayGroups():Promise<{items:GatewayGroup[];requestId:string}> {
  const response=await constructorRequest('/api/v1/gateway/groups')
  if(!response.ok)throw new Error(await apiErrorMessage(response,'Gateway groups unavailable'))
  const body=await response.json() as {items?:unknown;requestId?:unknown}
  if(!Array.isArray(body.items)||typeof body.requestId!=='string')throw new Error('Constructor returned an invalid Gateway group list')
  const seen=new Set<string>()
  const items=body.items.map((item):GatewayGroup=>{
    if(!item||typeof item!=='object')throw new Error('Constructor returned an invalid Gateway group')
    const value=item as Record<string,unknown>
    if(typeof value.id!=='string'||!gatewayIDPattern.test(value.id)||seen.has(value.id)||!['system','application'].includes(String(value.kind))||typeof value.active!=='boolean'||!['empty','ready'].includes(String(value.state)))throw new Error('Constructor returned an invalid Gateway group')
    if(!(value.currentRevision===null||typeof value.currentRevision==='string')||!(value.previousRevision===null||typeof value.previousRevision==='string'))throw new Error('Constructor returned an invalid Gateway group revision pointer')
    seen.add(value.id)
    return {id:value.id,kind:value.kind as GatewayGroup['kind'],active:value.active,currentRevision:value.currentRevision as string|null,previousRevision:value.previousRevision as string|null,state:value.state as GatewayGroup['state']}
  })
  return {items,requestId:body.requestId}
}
export async function listGatewayGroupReleases(groupID:string,options:{cursor?:string;limit?:number}={}):Promise<GatewayGroupRevisionPage> {
  const group=gatewayID(groupID,'Gateway group ID')
  const limit=options.limit??50
  if(!Number.isInteger(limit)||limit<1||limit>100)throw new Error('Gateway release page limit must be between 1 and 100')
  if(options.cursor!==undefined&&(options.cursor.length>4096||/[\u0000-\u001f\u007f]/.test(options.cursor)))throw new Error('Gateway release cursor is invalid')
  const query=new URLSearchParams({limit:String(limit)})
  if(options.cursor)query.set('cursor',options.cursor)
  const response=await constructorRequest(`/api/v1/gateway/groups/${group}/releases?${query}`)
  if(!response.ok)throw new Error(await apiErrorMessage(response,'Gateway group releases unavailable'))
  const body=await response.json() as {items?:unknown;nextCursor?:unknown;requestId?:unknown}
  if(!Array.isArray(body.items)||typeof body.requestId!=='string'||!(body.nextCursor===null||typeof body.nextCursor==='string'))throw new Error('Constructor returned an invalid Gateway release page')
  const seen=new Set<string>()
  const items=body.items.map((item):GatewayGroupRevisionSummary=>{
    if(!item||typeof item!=='object')throw new Error('Constructor returned an invalid Gateway revision summary')
    const value=item as Record<string,unknown>
    if(typeof value.id!=='string'||!/^([a-fA-F0-9]{64})$/.test(value.id)||seen.has(value.id)||value.groupId!==groupID||typeof value.caddyfileDigest!=='string'||!/^([a-fA-F0-9]{64})$/.test(value.caddyfileDigest)||!(value.artifactDigest===null||typeof value.artifactDigest==='string'&&/^([a-fA-F0-9]{64})$/.test(value.artifactDigest))||typeof value.createdAt!=='string'||!Number.isFinite(Date.parse(value.createdAt))||('actor'in value&&typeof value.actor!=='string'))throw new Error('Constructor returned an invalid Gateway revision summary')
    if('caddyfile'in value||'caddyfilePath'in value||'artifactPath'in value||'frontends'in value)throw new Error('Constructor returned revision content in a metadata-only response')
    seen.add(value.id)
    return {id:value.id,groupId:value.groupId as string,caddyfileDigest:value.caddyfileDigest,artifactDigest:value.artifactDigest as string|null,createdAt:value.createdAt,actor:typeof value.actor==='string'?value.actor:undefined}
  })
  return {items,nextCursor:body.nextCursor as string|null,requestId:body.requestId}
}
export async function loadPluginAdminSurface(instance:string):Promise<AdminSurface> {
  const response=await constructorRequest(`/api/plugins/${gatewayID(instance,'Plugin ID')}/admin/surface`)
  if(!response.ok)throw new Error(await apiErrorMessage(response,'Plugin Admin Surface unavailable'))
  const surface=parseAdminSurface(await response.json())
  const etag=response.headers.get('ETag')
  if(!etag)throw new Error('Gateway Admin Surface response is missing its digest ETag')
  const digest=etag.replace(/^W\//,'').replace(/^"|"$/g,'')
  if(digest!==surface.surfaceDigest)throw new Error('Gateway Surface ETag does not match its digest')
  return surface
}

export async function loadCaddyfile(projectID:string):Promise<CaddyfileSource> {
  if(!projectID)throw new Error('A project ID is required to load its Caddyfile')
  const response=await constructorRequest(`/api/v1/projects/${encodeURIComponent(projectID)}/files/${encodeURIComponent(caddyfilePath)}`)
  if(response.status===404)return {text:'',revision:'',diagnostics:[]}
  const text=await response.text()
  if(!response.ok)throw new Error(readCaddyfileFailure(text,response.status,'Caddyfile source unavailable').message)
  return {text,revision:response.headers.get('ETag')??'',diagnostics:[]}
}

export async function saveCaddyfile(projectID:string,text:string,revision:string):Promise<CaddyfileSource> {
  if(!projectID)throw new Error('A project ID is required to save its Caddyfile')
  const response=await constructorRequest(`/api/v1/projects/${encodeURIComponent(projectID)}/files/${encodeURIComponent(caddyfilePath)}`,{method:'PUT',headers:{'Content-Type':'text/plain; charset=utf-8','If-Match':revision},body:text})
  const responseText=await response.text()
  const failure=readCaddyfileFailure(responseText,response.status,'Caddyfile save failed')
  if(!response.ok)throw new CaddyfileSaveError(failure.message,response.status===409||response.status===412,failure.diagnostics)
  return {text,revision:response.headers.get('ETag')??failure.revision,diagnostics:failure.diagnostics}
}
export async function queryPluginAdmin(instance:string,page:string,digest:string,input:Record<string,unknown>):Promise<unknown> {
  const path=`/api/plugins/${gatewayID(instance,'Plugin ID')}/admin/pages/${gatewayID(page,'Page ID')}/query`
  const response=await constructorRequest(path,{method:'POST',headers:{'Content-Type':'application/json','If-Match':`"${digest}"`},body:JSON.stringify(input)})
  if(response.status===409)throw new PluginSurfaceChangedError()
  if(!response.ok)throw new Error(await apiErrorMessage(response,'Plugin Admin query failed'))
  return response.json()
}
export async function loadPluginAdminOptions(instance:string,page:string,digest:string,field:AdminField,input:Record<string,unknown>):Promise<Array<{value:string|number|boolean;label:string}>> {
  const source=field.optionsSource
  if(!source)throw new Error('This field has no declared dynamic options source')
  const errors=validateAdminInput(source.inputSchema,input)
  if(errors.length)throw new Error(errors.join('; '))
  const response=await queryPluginAdmin(instance,page,digest,{mode:'options',field:field.key,input}) as {items?:unknown}
  if(!response||typeof response!=='object'||!Array.isArray(response.items)||response.items.length>200)throw new Error('Gateway returned an invalid options result')
  const items=response.items.map((item):{value:string|number|boolean;label:string}=>{
    if(!item||typeof item!=='object')throw new Error('Gateway returned an invalid option')
    const option=item as Record<string,unknown>
    const value=option[source.valueField]
    const label=option[source.labelField]
    if(!(typeof value==='string'||typeof value==='boolean'||(typeof value==='number'&&Number.isFinite(value)))||typeof label!=='string'||!label.trim()||label.length>256)throw new Error('Gateway returned an invalid option')
    return {value,label}
  })
  return items
}
export async function runPluginAdminAction(instance:string,page:string,digest:string,action:AdminAction,input:Record<string,unknown>,confirm:(message:string)=>boolean=message=>window.confirm(message)):Promise<unknown> {
  if(!action.inputSchema)throw new Error('This action has no declared input schema and cannot be executed safely')
  const inputErrors=validateAdminInput(action.inputSchema,input)
  if(inputErrors.length)throw new Error(inputErrors.join('; '))
  const path=`/api/plugins/${gatewayID(instance,'Plugin ID')}/admin/pages/${gatewayID(page,'Page ID')}/actions/${gatewayID(action.id,'Action ID')}`
  const idempotencyKey=crypto.randomUUID()
  const body=JSON.stringify(input)
  const headers={'Content-Type':'application/json','If-Match':`"${digest}"`,'Idempotency-Key':idempotencyKey}
  const send=(confirmationToken?:string)=>constructorRequest(path,{method:'POST',headers:{...headers,...(confirmationToken?{'X-Admin-Confirmation':confirmationToken}:{})},body})
  let response=await send()
  if(response.status===409)throw new PluginSurfaceChangedError()
  if(action.dangerous&&response.status!==428)throw new Error('Gateway violated the dangerous-action confirmation contract; no confirmation retry was sent')
  if(action.dangerous&&response.status===428){
    const challenge=await response.json() as {code?:string;confirmationToken?:string;expiresAt?:string}
    if(challenge.code!=='confirmation_required'||typeof challenge.confirmationToken!=='string'||!challenge.confirmationToken||typeof challenge.expiresAt!=='string'||Date.parse(challenge.expiresAt)<=Date.now())throw new Error('Gateway returned an invalid or expired action confirmation challenge')
    if(!confirm(action.confirmation??action.title))return {cancelled:true}
    response=await send(challenge.confirmationToken)
    if(response.status===409)throw new PluginSurfaceChangedError()
  }
  if(!response.ok)throw new Error(await apiErrorMessage(response,'Plugin Admin action failed'))
  return response.status===204?null:response.json()
}

export async function listProjects(): Promise<ProjectRecord[]> {
  const response = await constructorRequest('/api/v1/projects')
  if (!response.ok) throw new Error(await apiErrorMessage(response,'Projects unavailable'))
  const projects=(await response.json() as {projects: ProjectRecord[]}).projects
  const seen=new Set<string>()
  for(const project of projects){
    if(seen.has(project.id))throw new Error(`Project registry returned duplicate project id: ${project.id}`)
    seen.add(project.id)
  }
  return projects
}
export async function createProject(id: string, name: string): Promise<ProjectRecord> {
  return post<ProjectRecord>('/api/v1/projects', {id, name})
}
export async function activateProject(id: string): Promise<ProjectRecord> {
  const body = await post<{project: ProjectRecord}>(`/api/v1/projects/${encodeURIComponent(id)}/activate`)
  return body.project
}
export async function activeProject(): Promise<ProjectRecord> {
  const response = await constructorRequest('/api/v1/projects/active')
  if (!response.ok) throw new Error(await apiErrorMessage(response,'Active project unavailable'))
  return (await response.json() as {project: ProjectRecord}).project
}

export async function startPreview(): Promise<PreviewSession> {
  const response = await constructorRequest('/api/v1/preview', {method:'POST'})
  if (!response.ok) throw new Error(await apiErrorMessage(response,`Preview start failed: ${response.status}`))
  return response.json() as Promise<PreviewSession>
}
export async function stopPreview(): Promise<void> {
  const response = await constructorRequest('/api/v1/preview', {method:'DELETE'})
  if (!response.ok) throw new Error(await apiErrorMessage(response,`Preview stop failed: ${response.status}`))
}

export async function loadProject(siteId:string,locale?:string): Promise<Project> {
  try {
    if(!siteId)throw new Error('A Site must be selected before loading project content')
    const response = await constructorRequest('/api/v1/project')
    if (!response.ok) throw new Error(await apiErrorMessage(response,`Project request failed: ${response.status}`))
    const manifest = await response.json() as ProjectManifest
    const sitePath=`liapoldus/sites/${siteId}.json`
    const siteResponse=await constructorRequest(`/api/v1/project/file?path=${encodeURIComponent(sitePath)}`)
    if(!siteResponse.ok) throw new Error(await apiErrorMessage(siteResponse,`Site document unavailable: ${siteResponse.status}`))
    const siteDocument=await siteResponse.json() as SiteDocument
    if(siteDocument.id!==siteId||siteDocument.projectId!==manifest.id) throw new Error('Site document does not belong to the active project')
    if(!siteDocument.locales.length) throw new Error('Site has no enabled locales')
    const activeLocale=locale==='default'?'default':locale&&siteDocument.locales.includes(locale)?locale:siteDocument.locales[0]
    const schemaEntries = await Promise.all((manifest.components ?? []).map(async name => {const id=name.toLowerCase().replace(/[^a-z0-9-]/g,'-');const schemaResponse=await constructorRequest(`/api/v1/project/file?path=${encodeURIComponent(`src/components/${id}/schema.json`)}`);if(schemaResponse.status===404)return null;if(!schemaResponse.ok)throw new Error(await apiErrorMessage(schemaResponse,`Schema unavailable: ${schemaResponse.status}`));return [name,await schemaResponse.json() as ComponentSchema] as const}))
    const schemas = Object.fromEntries(schemaEntries.filter((entry): entry is readonly [string, ComponentSchema] => entry !== null))
    const contentQuery=new URLSearchParams({siteId,locale:activeLocale})
    const contentResponse=await constructorRequest(`/api/v1/project/content?${contentQuery}`)
    if(!contentResponse.ok)throw new Error(await apiErrorMessage(contentResponse,`Content request failed: ${contentResponse.status}`))
    const contentView=await contentResponse.json() as {document:ContentDocument;contentRevision?:string;defaultContentRevision?:string;fallbackFrom?:string}
    const contentDocument=contentView.document
    const pageEntries=siteDocument.pages.map(page=>typeof page==='string'?[page,page] as const:[page.id,page.name] as const)
    const pageIDs=new Set<string>()
    for(const [id,name] of pageEntries){
      if(!/^[a-z][a-z0-9-]{1,62}$/.test(id)||!name.trim()||pageIDs.has(id))throw new Error('Site pages require unique stable IDs and non-empty display names')
      if(!manifest.pages.includes(id))throw new Error(`Site page ${id} is not declared by the project manifest`)
      pageIDs.add(id)
    }
    const normalizedSiteDocument={...siteDocument,pages:pageEntries.map(([id,name])=>({id,name}))}
    return {id:manifest.id,name:manifest.name,components:manifest.components,siteId,pageIDs:pageEntries.map(([id])=>id),sitePages:Object.fromEntries(pageEntries),siteDocument:normalizedSiteDocument,locales:siteDocument.locales,locale:activeLocale,contentDocument,schemas,contentRevision:contentView.contentRevision,defaultContentRevision:contentView.defaultContentRevision,...(contentView.fallbackFrom&&contentView.fallbackFrom!==activeLocale?{contentFallbackFrom:contentView.fallbackFrom}:{}),manifestRevision:response.headers.get('ETag')??undefined,siteRevision:siteResponse.headers.get('ETag')??undefined}
  } catch (error) {
    throw error instanceof Error?error:new Error('Project request failed')
  }
}
export async function listSiteDocuments():Promise<SiteDocument[]>{const response=await constructorRequest('/api/v1/project/sites');if(!response.ok)throw new Error(await apiErrorMessage(response,`Sites unavailable: ${response.status}`));const body=await response.json() as {sites:SiteDocument[]|null};return body.sites??[]}
export async function listProjectThemes():Promise<ThemeDocument[]>{const response=await constructorRequest('/api/v1/project/themes');if(!response.ok)throw new Error(await apiErrorMessage(response,`Themes unavailable: ${response.status}`));const body=await response.json() as {themes:ThemeDocument[]|null};return body.themes??[]}
export async function saveProjectTheme(theme:ThemeDocument,revision:string):Promise<string>{const {revision:_,...document}=theme;const path=`liapoldus/themes/${theme.id}.json`;const response=await constructorRequest(`/api/v1/project/file?path=${encodeURIComponent(path)}`,{method:'PUT',headers:{'Content-Type':'application/json','If-Match':revision},body:JSON.stringify(document,null,2)});if(response.status===409||response.status===412)throw new Error('Theme changed on disk. Reload it before retrying this edit.');if(!response.ok)throw new Error(await apiErrorMessage(response,`Theme update failed: ${response.status}`));return response.headers.get('ETag')??''}
export async function listProjectAssets():Promise<AssetItem[]>{const response=await constructorRequest('/api/v1/project/assets');if(!response.ok)throw new Error(await apiErrorMessage(response,`Assets unavailable: ${response.status}`));const body=await response.json() as {assets:AssetItem[]|null};return body.assets??[]}
export async function uploadProjectAsset(file:File):Promise<AssetUploadResult>{if(file.size<=0)throw new Error('Choose a non-empty image file');if(file.size>25*1024*1024)throw new Error('Asset uploads are limited to 25 MiB');const response=await constructorRequest('/api/v1/project/assets',{method:'POST',body:file});if(!response.ok)throw new Error(await apiErrorMessage(response,'Asset upload failed'));return response.json() as Promise<AssetUploadResult>}
export function createProjectSite(request:{id:string;name:string;sourceSiteId:string;revisions:{manifest:string;sourceSite:string}}):Promise<{site:SiteDocument}>{return post('/api/v1/project/sites',request)}
export function createProjectPage(request:{siteId:string;id:string;name:string;routePath:string;revisions:{manifest:string;site:string;routes:string}}):Promise<{page:SitePage}>{return post('/api/v1/project/pages',request)}
export async function saveSiteDocument(document:SiteDocument,revision:string):Promise<string>{const path=`liapoldus/sites/${document.id}.json`;const response=await constructorRequest(`/api/v1/project/file?path=${encodeURIComponent(path)}`,{method:'PUT',headers:{'Content-Type':'application/json','If-Match':revision},body:JSON.stringify(document,null,2)});if(response.status===409||response.status===412)throw new Error('Site changed on disk. Reload it before retrying this page edit.');if(!response.ok)throw new Error(await apiErrorMessage(response,`Site update failed: ${response.status}`));return response.headers.get('ETag')??''}
export class ContentRevisionConflict extends Error {
  constructor() { super('Content changed on disk since it was loaded. Your draft is still available; reload or reconcile before saving again.'); this.name='ContentRevisionConflict' }
}
export async function saveProjectContent(content: ContentDocument, base:ContentDocument, siteId:string,locale:string,revision?: string,defaultRevision?:string): Promise<{contentRevision:string;defaultContentRevision:string}> {
  if(!siteId||!locale)throw new Error('Site and locale are required to save content')
  const query=new URLSearchParams({siteId,locale})
  const response = await constructorRequest(`/api/v1/project/content?${query}`, {method:'PUT',headers:{'Content-Type':'application/json'},body:JSON.stringify({base,document:content,revisions:{locale:revision??'',default:defaultRevision??''}},null,2)})
  if (response.status===409||response.status===412) throw new ContentRevisionConflict()
  if (!response.ok) throw new Error(await apiErrorMessage(response,`Content save failed: ${response.status}`))
  return response.json() as Promise<{contentRevision:string;defaultContentRevision:string}>
}
export async function loadRoutes(environment='development'):Promise<{document:RouteDocument;revision:string}>{const response=await constructorRequest(`/api/v1/project/routes?environment=${encodeURIComponent(environment)}`);if(response.status===404)return {document:{schemaVersion:1,id:`${environment}-routes`,routes:[]},revision:''};if(!response.ok)throw new Error(await apiErrorMessage(response,'Route document unavailable'));return {document:await response.json() as RouteDocument,revision:response.headers.get('ETag')??''}}
export async function saveRoutes(document:RouteDocument,revision:string,environment='development'):Promise<string>{const response=await constructorRequest(`/api/v1/project/routes?environment=${encodeURIComponent(environment)}`,{method:'PUT',headers:{'Content-Type':'application/json','If-Match':revision},body:JSON.stringify(document,null,2)});if(!response.ok)throw new Error(await apiErrorMessage(response,'Route save failed'));return response.headers.get('ETag')??''}
export async function generateRoutes(environment='development'):Promise<void>{const response=await constructorRequest(`/api/v1/project/routes/generate?environment=${encodeURIComponent(environment)}`,{method:'POST'});if(!response.ok)throw new Error(await apiErrorMessage(response,'Route generation failed'))}
export async function validateProject(siteId?:string,locale?:string): Promise<{valid: boolean; diagnostics: Diagnostic[]}> { if(Boolean(siteId)!==Boolean(locale))throw new Error('Site and locale must be provided together');const query=new URLSearchParams();if(siteId&&locale){query.set('siteId',siteId);query.set('locale',locale)}const response=await constructorRequest(`/api/v1/project/validate${query.size?`?${query}`:''}`,{method:'POST'});const body=await response.json() as {valid?:boolean;diagnostics?:Diagnostic[]};return {valid:body.valid===true,diagnostics:body.diagnostics??[]} }
export async function generateLocale(siteId:string,locale:string): Promise<void> { if(!siteId||!locale)throw new Error('Site and locale are required to generate content');const query=new URLSearchParams({siteId,locale});const response=await constructorRequest(`/api/v1/project/generate?${query}`,{method:'POST'});if(!response.ok)throw new Error(await apiErrorMessage(response,'Generation failed')) }
export async function commitProject(message: string): Promise<string> { const response=await constructorRequest('/api/v1/git/commit',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({message})}); if(!response.ok) throw new Error(await apiErrorMessage(response,'Commit failed')); const body=await response.json() as {revision:string}; return body.revision }
export async function createGitBranch(name:string):Promise<GitBranch>{if(!name.trim())throw new Error('Branch name is required');return post<{branch:GitBranch}>('/api/v1/git/branches',{name}).then(result=>result.branch)}
export async function checkoutGitBranch(name:string):Promise<GitBranch>{if(!name.trim())throw new Error('Branch name is required');return post<{branch:GitBranch}>('/api/v1/git/checkout',{name}).then(result=>result.branch)}
export async function loadGitStatus():Promise<string[]> {const response=await constructorRequest('/api/v1/git/status');if(!response.ok)throw new Error(await apiErrorMessage(response,'Git status unavailable'));const body=await response.json() as {files:string[]|null};return body.files??[]}
export async function loadGitDiff():Promise<string> {const response=await constructorRequest('/api/v1/git/diff');if(!response.ok)throw new Error(await apiErrorMessage(response,'Git diff unavailable'));const body=await response.json() as {diff:string};return body.diff??''}
export async function loadGitBranches():Promise<GitBranch[]> {const response=await constructorRequest('/api/v1/git/branches');if(!response.ok)throw new Error(await apiErrorMessage(response,'Git branches unavailable'));const body=await response.json() as {branches:GitBranch[]|null};return body.branches??[]}
export async function loadGitHistory():Promise<GitCommitRecord[]> {const response=await constructorRequest('/api/v1/git/history');if(!response.ok)throw new Error(await apiErrorMessage(response,'Git history unavailable'));const body=await response.json() as {commits:GitCommitRecord[]|null};return body.commits??[]}
async function post<T>(url:string,body?:unknown):Promise<T>{const response=await constructorRequest(url,{method:'POST',headers:{'Content-Type':'application/json'},...(body===undefined?{}:{body:JSON.stringify(body)})});if(!response.ok)throw new Error(await apiErrorMessage(response,`Request failed: ${response.status}`));return response.json() as Promise<T>}
export function saveEnvironment(environment:Environment){return post<Environment>('/api/v1/environments',environment)}
export function createSnapshot(siteId:string,locale:string){if(!siteId||!locale)throw new Error('Site and locale are required to create a snapshot');return post<Snapshot>(`/api/v1/snapshots?siteId=${encodeURIComponent(siteId)}&locale=${encodeURIComponent(locale)}`)}
export function buildSnapshot(snapshotId:string){return post<Build>(`/api/v1/snapshots/${encodeURIComponent(snapshotId)}/builds`)}
export async function deploymentTargetState(siteId:string,environmentId:string):Promise<DeploymentTargetState>{const query=new URLSearchParams({siteId,environmentId});const response=await constructorRequest(`/api/v1/deployment-target?${query}`);if(!response.ok)throw new Error(await apiErrorMessage(response,'Deployment target state unavailable'));return response.json() as Promise<DeploymentTargetState>}
export function deploySnapshot(snapshotId:string,buildId:string,siteId:string,environmentId:string,confirmedGatewayRevision?:string){if(!siteId||!environmentId)throw new Error('Site and environment are required to deploy a snapshot');return post<Deployment>('/api/v1/deployments',{id:`deployment-${snapshotId}-${environmentId}`,siteId,environmentId,snapshotId,buildId,confirmedTarget:`${siteId}/${environmentId}`,...(confirmedGatewayRevision===undefined?{}:{confirmedGatewayRevision})})}
export function rollbackDeployment(deploymentId:string,siteId:string,environmentId:string){if(!siteId||!environmentId)throw new Error('Site and environment are required to confirm a rollback target');const query=new URLSearchParams({confirmedTarget:`${siteId}/${environmentId}`});return post<Deployment>(`/api/v1/deployments/${encodeURIComponent(deploymentId)}/rollback?${query}`)}
export async function loadDeliveryHistory(){const [snapshotsResponse,buildsResponse,deploymentsResponse]=await Promise.all([constructorRequest('/api/v1/snapshots'),constructorRequest('/api/v1/builds'),constructorRequest('/api/v1/deployments')]);for(const response of [snapshotsResponse,buildsResponse,deploymentsResponse])if(!response.ok)throw new Error(await apiErrorMessage(response,'Delivery history unavailable'));const snapshots=await snapshotsResponse.json() as {snapshots:Snapshot[]|null};const builds=await buildsResponse.json() as {builds:Build[]|null};const deployments=await deploymentsResponse.json() as {deployments:Deployment[]|null};return {snapshots:snapshots.snapshots??[],builds:builds.builds??[],deployments:deployments.deployments??[]}}
export async function loadRBAC(){const [usersResponse,rolesResponse,permissionsResponse]=await Promise.all([constructorRequest('/api/v1/users'),constructorRequest('/api/v1/roles'),constructorRequest('/api/v1/permissions')]);for(const response of [usersResponse,rolesResponse,permissionsResponse])if(!response.ok)throw new Error(await apiErrorMessage(response,'RBAC unavailable'));const users=await usersResponse.json() as {users:User[]|null};const roles=await rolesResponse.json() as {roles:Role[]|null};const permissions=await permissionsResponse.json() as {permissions:Permission[]|null};return {users:users.users??[],roles:roles.roles??[],permissions:permissions.permissions??[]}}
export function createUser(value:User){return post<User>('/api/v1/users',value)}
export function createRole(value:Role){return post<Role>('/api/v1/roles',value)}
export function createPermission(value:Permission){return post<Permission>('/api/v1/permissions',value)}
export function assignRole(value:{userId:string;roleId:string}){return post('/api/v1/user-roles',value)}
export function grantPermission(value:{roleId:string;permissionKey:string}){return post('/api/v1/role-permissions',value)}
