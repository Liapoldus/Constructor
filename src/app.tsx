import {create} from 'zustand'
import {useEffect, useRef, useState} from 'react'
import {activateProject, activeProject, assignRole, buildSnapshot, CaddyfileSaveError, ContentRevisionConflict, createPermission, createProject, createProjectPage, createProjectSite, createRole, createSnapshot, createUser, deploySnapshot, deploymentTargetState, generateRoutes, grantPermission, listProjectAssets, listProjects, listProjectThemes, listSiteDocuments, loadCaddyfile, loadDeliveryHistory, loadProject, loadRBAC, loadRoutes, rollbackDeployment, saveCaddyfile, saveEnvironment, saveProjectContent, saveProjectTheme, saveRoutes, saveSiteDocument, startPreview, stopPreview, toRuntimeContent, uploadProjectAsset, validateProject, type AssetItem, type Diagnostic, type Environment, type PreviewDraftMessage, type Project, type RouteDocument, type RuntimeContent, type SiteDocument, type SitePage, type ThemeDocument, type ThemeTokenType} from './api'
import {caddyfileDraftForProject,createCaddyfileDraft,createCaddyfileDraftCache,editCaddyfileDraft,emptyCaddyfileDraft,failCaddyfileSave,finishCaddyfileSave,startCaddyfileSave,type CaddyfileDraft} from './caddyfile-draft'
import {CaddyfileEditorView} from './caddyfile-editor-view'
import {redoHistory, recordHistory, undoHistory, type HistoryEntry} from './editor-history'
import {sortProblemsBySeverity, type EditorProblem} from './editor-problems'
import {previewViewports, previewViewportStyle, type PreviewViewport} from './responsive-preview'
import {disableSiteLocale, enableSiteLocale} from './site-locales'
import {mergeContentDocuments} from './content-merge'
import {AssetField} from './asset-field'
import {GitPanel} from './git-panel'
import {PluginAdminPanel} from './plugin-admin-panel'

type ContentDocument = Project['contentDocument']
type EditorSelection = {selected:string;selectedPageID:string;selectedInstanceID:string;problems:EditorProblem[]}
type EditorState = {project: Project; selected: string; selectedPageID: string; selectedInstanceID: string; preview: boolean; problems: EditorProblem[]; contentDirty: boolean; saveStatus: 'saved'|'dirty'|'saving'|'conflict'|'error'; savedContent:ContentDocument; past:HistoryEntry<ContentDocument,EditorSelection>[]; future:HistoryEntry<ContentDocument,EditorSelection>[]; setProject: (project: Project) => void; setContent: (key: string, value: unknown) => void; markSaved:(document:ContentDocument,revisions:{contentRevision:string;defaultContentRevision:string})=>void; select: (id: string) => void; selectInstance: (instanceId: string) => void; addInstance: (component: string) => void; deleteInstance: (instanceId: string) => void; undo:()=>void; redo:()=>void; togglePreview: () => void}
type CachedDraft = Pick<EditorState,'project'|'selected'|'selectedPageID'|'selectedInstanceID'|'savedContent'|'past'|'future'|'contentDirty'|'saveStatus'>
const pageName = (project:Project, pageID:string) => project.sitePages[pageID] ?? pageID
const draftKey = (project:Project) => JSON.stringify([project.id,project.siteId,project.locale])
const hasDraft = (projectId:string,siteId:string,locale:string) => draftCache.has(JSON.stringify([projectId,siteId,locale]))
const draftCache = new Map<string,CachedDraft>()
const problemCache = new Map<string,EditorProblem[]>()
export function clearEditorBranchCaches(){draftCache.clear();problemCache.clear()}
const documentIsDirty = (document:ContentDocument, saved:ContentDocument) => JSON.stringify(document)!==JSON.stringify(saved)
const toEditorProblem = (diagnostic:Diagnostic):EditorProblem => ({id:JSON.stringify([diagnostic.code,diagnostic.path,diagnostic.pageId,diagnostic.instanceId,diagnostic.fieldKey,diagnostic.message]),...diagnostic})
function recordContentEdit(state:EditorState, document:ContentDocument, group?:string) {
  const now=Date.now()
  const selection={selected:state.selected,selectedPageID:state.selectedPageID,selectedInstanceID:state.selectedInstanceID,problems:state.problems}
  const past=recordHistory(state.past,state.project.contentDocument,selection,state.future.length===0?group:undefined,now)
  const dirty=documentIsDirty(document,state.savedContent)
  return {project:{...state.project,contentDocument:document},past,future:[],contentDirty:dirty,saveStatus:state.saveStatus==='saving'?'saving' as const:dirty?'dirty' as const:'saved' as const}
}
export const useEditor = create<EditorState>((set) => ({
  project: {id:'',siteId:'',name: 'Loading…', pageIDs:[], sitePages:{}, siteDocument:{schemaVersion:1,id:'',projectId:'',name:'',pages:[],locales:[]}, components: [], locales:[], locale:'', contentDocument:{schemaVersion:1,id:'',instances:[]}, schemas: {}},
  selected: 'Hero', selectedPageID:'home', selectedInstanceID:'hero-main', preview: false, problems: [], contentDirty:false, saveStatus:'saved',savedContent:{schemaVersion:1,id:'home-content',instances:[]},past:[],future:[],
  setProject: project => set(state => {
    const key=draftKey(project)
    const cached=draftCache.get(key)
    const problems=problemCache.get(key)??[]
    if(cached)return {...cached,problems,project:{...cached.project,...project,contentDocument:cached.project.contentDocument,contentRevision:cached.project.contentRevision,defaultContentRevision:cached.project.defaultContentRevision}}
    const firstInstance = project.contentDocument.instances[0]
    const nextPageID = firstInstance?.pageId ?? project.pageIDs[0] ?? 'home'
    return {project, selectedPageID:nextPageID, selectedInstanceID:firstInstance?.id ?? '', selected:project.components.find(name => (project.schemas[name]?.id ?? name.toLowerCase()) === firstInstance?.component) ?? nextPageID ?? state.selected, problems, contentDirty:false, saveStatus:'saved',savedContent:project.contentDocument,past:[],future:[]}
  }),
  markSaved:(document,revisions)=>set(state=>{
    const unchanged=JSON.stringify(state.project.contentDocument)===JSON.stringify(document)
    return {project:{...state.project,...revisions,contentFallbackFrom:undefined},savedContent:document,contentDirty:!unchanged,saveStatus:unchanged?'saved':'dirty'}
  }),
  setContent: (key, value) => set(state => {
    const existing = state.project.contentDocument.instances.find(instance => instance.id === state.selectedInstanceID)
    if (!existing) return state
    if(JSON.stringify(existing.fields[key])===JSON.stringify(value))return state
    const instances = state.project.contentDocument.instances.map(item => item.id === existing.id ? {...item, fields:{...item.fields, [key]:value}} : item)
    const contentDocument = {...state.project.contentDocument, instances}
    return {...recordContentEdit(state,contentDocument,`${existing.id}:${key}`),problems:state.problems.filter(problem=>!(problem.instanceId===existing.id&&problem.fieldKey===key))}
  }),
  select: selected => set(state => {
    if (state.project.pageIDs.includes(selected)) {
      const selectedPageID = selected
      const instance = state.project.contentDocument.instances.find(value => value.pageId === selectedPageID)
      const componentName = instance && state.project.components.find(name => (state.project.schemas[name]?.id ?? name.toLowerCase()) === instance.component)
      return {selected:pageName(state.project,selected), selectedPageID, selectedInstanceID:instance?.id ?? '', ...(componentName ? {selected:componentName} : {})}
    }
    const component = state.project.schemas[selected]?.id ?? selected.toLowerCase()
    const instance = state.project.contentDocument.instances.find(value => value.pageId === state.selectedPageID && value.component === component)
    return {selected, selectedInstanceID:instance?.id ?? ''}
  }),
  selectInstance: instanceId => set(state => {
    const instance = state.project.contentDocument.instances.find(value => value.id === instanceId)
    if (!instance) return state
    const componentName = state.project.components.find(name => (state.project.schemas[name]?.id ?? name.toLowerCase()) === instance.component) ?? instance.component
    return {selected:componentName, selectedPageID:instance.pageId, selectedInstanceID:instance.id}
  }),
  addInstance: componentName => set(state => {
    const schema = state.project.schemas[componentName]
    const component = schema?.id ?? componentName.toLowerCase()
    const fields = Object.fromEntries((schema?.fields ?? []).filter(field => field.default !== undefined || field.required).map(field => [field.key, field.default ?? '']))
    const instance = {id:crypto.randomUUID(), pageId:state.selectedPageID, component, fields}
    const contentDocument = {...state.project.contentDocument, instances:[...state.project.contentDocument.instances, instance]}
    return {...recordContentEdit(state,contentDocument),selected:componentName,selectedInstanceID:instance.id}
  }),
  deleteInstance: instanceId => set(state => {
    const instances=state.project.contentDocument.instances.filter(instance=>instance.id!==instanceId)
    if(instances.length===state.project.contentDocument.instances.length)return state
    const contentDocument={...state.project.contentDocument,instances}
    const next=instances.find(instance=>instance.pageId===state.selectedPageID)
    const componentName=next&&state.project.components.find(name=>(state.project.schemas[name]?.id??name.toLowerCase())===next.component)
    return {...recordContentEdit(state,contentDocument),selectedInstanceID:next?.id??'',problems:state.problems.filter(problem=>problem.instanceId!==instanceId),...(componentName?{selected:componentName}:{})}
  }),
  undo:()=>set(state=>{
    const selection={selected:state.selected,selectedPageID:state.selectedPageID,selectedInstanceID:state.selectedInstanceID,problems:state.problems}
    const step=undoHistory(state.project.contentDocument,selection,state.past,state.future)
    if(!step)return state
    const dirty=documentIsDirty(step.document,state.savedContent)
    return {project:{...state.project,contentDocument:step.document},...step.selection,past:step.past,future:step.future,contentDirty:dirty,saveStatus:state.saveStatus==='saving'?'saving':dirty?'dirty':'saved'}
  }),
  redo:()=>set(state=>{
    const selection={selected:state.selected,selectedPageID:state.selectedPageID,selectedInstanceID:state.selectedInstanceID,problems:state.problems}
    const step=redoHistory(state.project.contentDocument,selection,state.past,state.future)
    if(!step)return state
    const dirty=documentIsDirty(step.document,state.savedContent)
    return {project:{...state.project,contentDocument:step.document},...step.selection,past:step.past,future:step.future,contentDirty:dirty,saveStatus:state.saveStatus==='saving'?'saving':dirty?'dirty':'saved'}
  }),
  togglePreview: () => set(state => ({preview: !state.preview}))
}))
useEditor.subscribe(state=>{
  if(!state.project.id||!state.project.siteId)return
  const key=draftKey(state.project)
  problemCache.set(key,state.problems)
  if(state.contentDirty)draftCache.set(key,{project:state.project,selected:state.selected,selectedPageID:state.selectedPageID,selectedInstanceID:state.selectedInstanceID,savedContent:state.savedContent,past:state.past,future:state.future,contentDirty:true,saveStatus:state.saveStatus})
  else draftCache.delete(key)
})

function Explorer({onSiteUpdated}:{onSiteUpdated:(site:SiteDocument)=>void}) {
  const {project, selected, selectedPageID, selectedInstanceID, select, selectInstance, addInstance} = useEditor()
  const [pageID,setPageID]=useState('')
  const [pageNameInput,setPageNameInput]=useState('')
  const [routePath,setRoutePath]=useState('')
  const [pageError,setPageError]=useState('')
  const [pageBusy,setPageBusy]=useState(false)
  const [editingPageID,setEditingPageID]=useState('')
  const [editingPageName,setEditingPageName]=useState('')
  const [siteBusy,setSiteBusy]=useState(false)
  const persistSiteDocument=async(document:SiteDocument)=>{
    const current=useEditor.getState().project
    if(!current.siteRevision)throw new Error('Reload the project to obtain the current Site revision.')
    setSiteBusy(true);setPageError('')
    try{
      await saveSiteDocument(document,current.siteRevision)
      const updated=await loadProject(current.siteId,current.locale)
      const selection=useEditor.getState()
      const selectedPageID=selection.selectedPageID
      const selectedInstanceID=selection.selectedInstanceID
      useEditor.getState().setProject(updated)
      if(updated.pageIDs.includes(selectedPageID)){
        const selectedInstance=updated.contentDocument.instances.find(instance=>instance.id===selectedInstanceID&&instance.pageId===selectedPageID)
        if(selectedInstance)useEditor.getState().selectInstance(selectedInstance.id)
        else useEditor.getState().select(selectedPageID)
      }
      onSiteUpdated(updated.siteDocument)
    }catch(error){setPageError(error instanceof Error?error.message:'Site page update failed');throw error}
    finally{setSiteBusy(false)}
  }
  const movePage=async(pageID:string,direction:-1|1)=>{
    const current=useEditor.getState().project
    const pages=current.siteDocument.pages as SitePage[]
    const index=pages.findIndex(page=>page.id===pageID)
    const target=index+direction
    if(index<0||target<0||target>=pages.length)return
    const reordered=[...pages]
    ;[reordered[index],reordered[target]]=[reordered[target],reordered[index]]
    await persistSiteDocument({...current.siteDocument,pages:reordered})
  }
  const renamePage=async(event:React.FormEvent)=>{
    event.preventDefault()
    const current=useEditor.getState().project
    const pages=(current.siteDocument.pages as SitePage[]).map(page=>page.id===editingPageID?{...page,name:editingPageName.trim()}:page)
    if(!editingPageName.trim())return
    try{await persistSiteDocument({...current.siteDocument,pages});setEditingPageID('');setEditingPageName('')}
    catch{/* The shared page error remains visible. */}
  }
  const removePage=async(pageID:string)=>{
    const current=useEditor.getState().project
    const page= (current.siteDocument.pages as SitePage[]).find(value=>value.id===pageID)
    if(!page)return
    const instanceCount=current.contentDocument.instances.filter(instance=>instance.pageId===pageID).length
    if(instanceCount){setPageError(`Cannot remove “${page.name}” while ${instanceCount} content instance(s) reference it. Remove and save those instances first.`);return}
    if(!window.confirm(`Remove “${page.name}” from Site “${current.siteDocument.name}”? The shared page ID and source file will be preserved. Content instances or routes that still reference it will block the change.`))return
    const pages=(current.siteDocument.pages as SitePage[]).filter(value=>value.id!==pageID)
    try{await persistSiteDocument({...current.siteDocument,pages});setEditingPageID('')}
    catch{/* The shared page error remains visible. */}
  }
  const createPage=async(event:React.FormEvent)=>{
    event.preventDefault()
    const current=useEditor.getState().project
    if(!current.manifestRevision||!current.siteRevision){setPageError('Reload the project to obtain current document revisions.');return}
    setPageBusy(true);setPageError('')
    try{
      const routes=await loadRoutes()
      const result=await createProjectPage({siteId:current.siteId,id:pageID.trim(),name:pageNameInput.trim(),routePath:routePath.trim(),revisions:{manifest:current.manifestRevision,site:current.siteRevision,routes:routes.revision}})
      await generateRoutes()
      const updated=await loadProject(current.siteId,current.locale)
      useEditor.getState().setProject(updated)
      useEditor.getState().select(result.page.id)
      setPageID('');setPageNameInput('');setRoutePath('')
    }catch(error){setPageError(error instanceof Error?error.message:'Page creation failed')}
    finally{setPageBusy(false)}
  }
  return <aside className="explorer"><h2>Constructor</h2><small>{project.name}</small><h3>Pages</h3>
    {pageError&&<div className="problem" role="alert">{pageError}</div>}
    {project.pageIDs.map((id,index) => <div className="page-row" key={id}>
      <button className={`tree ${selectedPageID===id?'active':''}`} onClick={() => select(id)}>▸ {pageName(project,id)}</button>
      <div className="page-actions">
        <button aria-label={`Move ${pageName(project,id)} up`} disabled={siteBusy||index===0} onClick={()=>void movePage(id,-1).catch(()=>{})}>↑</button>
        <button aria-label={`Move ${pageName(project,id)} down`} disabled={siteBusy||index===project.pageIDs.length-1} onClick={()=>void movePage(id,1).catch(()=>{})}>↓</button>
        <button aria-label={`Rename ${pageName(project,id)}`} disabled={siteBusy} onClick={()=>{setEditingPageID(id);setEditingPageName(pageName(project,id));setPageError('')}}>✎</button>
        <button aria-label={`Remove ${pageName(project,id)} from Site`} disabled={siteBusy||project.pageIDs.length===1} onClick={()=>void removePage(id)}>×</button>
      </div>
      {editingPageID===id&&<form className="page-rename" onSubmit={renamePage}>
        <label htmlFor={`page-name-${id}`}>Page name</label><input id={`page-name-${id}`} required maxLength={160} value={editingPageName} onChange={event=>setEditingPageName(event.target.value)}/>
        <button type="submit" disabled={siteBusy}>Save name</button><button type="button" disabled={siteBusy} onClick={()=>setEditingPageID('')}>Cancel</button>
      </form>}
    </div>)}
    <details className="page-create"><summary>Add page</summary><form onSubmit={createPage}>
      <label>Stable ID<input required pattern="[a-z][a-z0-9-]{1,62}" value={pageID} onChange={event=>setPageID(event.target.value)} placeholder="about"/></label>
      <label>Display name<input required maxLength={160} value={pageNameInput} onChange={event=>setPageNameInput(event.target.value)} placeholder="About us"/></label>
      <label>React route<input required value={routePath} onChange={event=>setRoutePath(event.target.value)} placeholder="/about"/></label>
      <button type="submit" disabled={pageBusy||siteBusy}>{pageBusy?'Creating…':'Create page'}</button>
    </form></details>
    <h3>Components</h3>{project.components.map(component => {
      const componentID=project.schemas[component]?.id??component.toLowerCase()
      const instances=project.contentDocument.instances.filter(instance=>instance.pageId===selectedPageID&&instance.component===componentID)
      return <div className="component-tree" key={component}><div className="component-heading"><span>◇ {component}</span><button aria-label={`Add ${component} instance`} onClick={()=>addInstance(component)}>+</button></div>
        {instances.map((instance,index)=><button className={`tree instance ${selectedInstanceID===instance.id?'active':''}`} key={instance.id} onClick={()=>selectInstance(instance.id)}>{component} #{index+1} <small>{instance.id}</small></button>)}
        {!instances.length&&<button className={`tree ${selected===component?'active':''}`} onClick={()=>select(component)}>Select schema</button>}
      </div>
    })}<h3>Project</h3><div className="muted">Git · Snapshots · Builds</div></aside>
}
function Inspector() {
  const {project, selected, selectedInstanceID, setContent, deleteInstance} = useEditor()
  const schema=project.schemas[selected]
  const instance=project.contentDocument.instances.find(value=>value.id===selectedInstanceID)
  const [parseError,setParseError]=useState('')
  const [assets,setAssets]=useState<AssetItem[]>([])
  const [assetError,setAssetError]=useState('')
  useEffect(()=>setParseError(''),[selectedInstanceID])
  useEffect(()=>{
    if(!project.id){setAssets([]);setAssetError('');return}
    let current=true
    void listProjectAssets().then(value=>{if(current){setAssets(value);setAssetError('')}}).catch(error=>{if(current)setAssetError(error instanceof Error?error.message:'Asset registry unavailable')})
    return()=>{current=false}
  },[project.id])
  return <aside className="inspector">
    <h3>Inspector</h3>
    <div className="muted">{instance?`${instance.component} · ${instance.id}`:`${selected} · schema`} · schema v{schema?.schemaVersion??1}</div>
    {schema&&instance?schema.fields.map(field=>{
      const value=Object.hasOwn(instance.fields,field.key)?instance.fields[field.key]:field.default??''
      const fieldId=`inspector-${encodeURIComponent(instance.id)}-${encodeURIComponent(field.key)}`
      const invalid=useEditor.getState().problems.some(problem=>problem.instanceId===instance.id&&problem.fieldKey===field.key&&problem.severity==='error')
      const structured=['object','array','image','icon','file','reference'].includes(field.type)
      const selectedOption=field.allowedValues?.findIndex(option=>Object.is(option,value))??-1
      const assetReference=value&&typeof value==='object'&&'id' in value?value as {id:string;alt?:string}:undefined
      return <label key={`${instance.id}:${field.key}`}>{field.label}
        {field.type==='select'&&field.allowedValues
          ? <select id={fieldId} aria-invalid={invalid} value={selectedOption<0?'':String(selectedOption)} onChange={event=>{const option=field.allowedValues?.[Number(event.target.value)];if(option!==undefined)setContent(field.key,option)}}><option value="" disabled>Select…</option>{field.allowedValues.map((option,index)=><option key={index} value={String(index)}>{String(option)}</option>)}</select>
          : field.type==='rich-text'
            ? <textarea id={fieldId} aria-invalid={invalid} value={String(value)} onChange={event=>setContent(field.key,event.target.value)}/>
            : field.type==='image'||field.type==='icon'||field.type==='file'
              ? <AssetField
                  assets={assets}
                  error={assetError}
                  fieldId={fieldId}
                  fieldLabel={field.label}
                  fieldType={field.type}
                  invalid={invalid}
                  nullable={field.nullable}
                  value={value===null?null:assetReference}
                  onUpload={async file=>{
                    const result=await uploadProjectAsset(file)
                    setAssets(current=>current.some(asset=>asset.id===result.asset.id)?current:[...current,result.asset])
                    void listProjectAssets().then(updated=>{setAssets(updated);setAssetError('')}).catch(error=>setAssetError(error instanceof Error?error.message:'Asset catalog refresh failed'))
                    return result.asset
                  }}
                  onChange={next=>setContent(field.key,next)}
                />
            : structured
              ? <textarea id={fieldId} aria-label={`${field.label} JSON`} aria-invalid={invalid} defaultValue={JSON.stringify(value,null,2)} onBlur={event=>{try{setContent(field.key,JSON.parse(event.target.value));setParseError('')}catch{setParseError(`${field.label}: invalid JSON`)}}}/>
              : <input id={fieldId} type="text" aria-invalid={invalid} value={value===null?'':String(value)} required={field.required} onChange={event=>setContent(field.key,event.target.value)}/>}
      <small>{field.type} · {field.localized?`localized · ${project.locale}`:'shared · default'}{field.required?' · required':''}{field.nullable?' · nullable':''}</small>
      </label>
    }):<div className="muted">Select a page instance, or add a component instance from Explorer.</div>}
    {instance&&<button className="danger" onClick={()=>{if(window.confirm(`Delete ${instance.component} instance? This change is not saved until you save the project.`))deleteInstance(instance.id)}}>Delete instance</button>}
    {parseError&&<div className="problem">{parseError}</div>}
    <div className="schema">{schema?.id??selected} · schema-driven</div>
  </aside>
}
function PreviewFrame({url, sessionId, content, selectedInstanceId}: {url: string; sessionId: string; content: RuntimeContent; selectedInstanceId:string}) {
  const frame = useRef<HTMLIFrameElement>(null)
  const revision = useRef(0)
  const currentContent = useRef(content)
  const currentSelection = useRef(selectedInstanceId)
  currentContent.current = content
  currentSelection.current = selectedInstanceId
  const sendDraft = () => {
    const target = frame.current?.contentWindow
    if (!target) return
    const message: PreviewDraftMessage = {protocol: 1, source: 'liapoldus.constructor', type: 'content-draft', sessionId, revision: ++revision.current, content: currentContent.current, selectedInstanceId:currentSelection.current||null}
    target.postMessage(message, '*')
  }
  useEffect(() => {
    const onReady = (event: MessageEvent<unknown>) => {
      if (event.source !== frame.current?.contentWindow || !isPreviewMessageOrigin(event.origin) || !isPreviewReady(event.data, sessionId)) return
      sendDraft()
      const acknowledgement = {protocol: 1, source: 'liapoldus.constructor', type: 'preview-ready-ack', sessionId}
      frame.current?.contentWindow?.postMessage(acknowledgement, '*')
    }
    window.addEventListener('message', onReady)
    return () => window.removeEventListener('message', onReady)
  }, [sessionId])
  useEffect(sendDraft, [content, sessionId])
  return <iframe ref={frame} title="Isolated project preview" sandbox="allow-scripts" referrerPolicy="no-referrer" src={url} onLoad={sendDraft}/>
}
function isPreviewReady(value: unknown, sessionId: string): boolean {
  if (!value || typeof value !== 'object') return false
  const message = value as Partial<{protocol: number; source: string; type: string; sessionId: string}>
  return message.protocol === 1 && message.source === 'liapoldus.constructor' && message.type === 'preview-ready' && message.sessionId === sessionId
}
function isPreviewMessageOrigin(origin: string): boolean {
  if (origin === 'null') return true
  try {
    const url = new URL(origin)
    const host = url.hostname.toLowerCase().replace(/^\[|\]$/g, '')
    return (url.protocol === 'http:' || url.protocol === 'https:') && (host === 'localhost' || host.endsWith('.localhost') || host === '::1' || /^127(?:\.\d{1,3}){3}$/.test(host))
  } catch { return false }
}
function Canvas() {
  const {project, preview, togglePreview, selectedPageID, selectedInstanceID} = useEditor()
  const [viewport,setViewport]=useState<PreviewViewport>('desktop')
  const [previewSession, setPreviewSession] = useState<{url:string;sessionId:string}|null>(null)
  const [previewError, setPreviewError] = useState('')
  const [starting, setStarting] = useState(false)
  const toggle = async () => {
    if (starting) return
    setStarting(true)
    try {
      if (preview) {
        setPreviewSession(null)
        togglePreview()
        await stopPreview()
      } else {
        const session = await startPreview()
        const url = new URL(session.url)
        url.searchParams.set('__liapoldus_preview_session', session.sessionId)
        setPreviewSession({...session, url: url.toString()})
        setPreviewError('')
        togglePreview()
      }
    } catch (error) {
      setPreviewError(error instanceof Error ? error.message : 'Preview operation failed')
    } finally {
      setStarting(false)
    }
  }
  const selectedPage=pageName(project,selectedPageID||project.pageIDs[0]||'home')
  return (
    <main className="workspace">
      <header>
        <span>Workspace / {selectedPage}</span>
        <div className="viewport-controls" role="group" aria-label="Preview viewport">
          {(Object.keys(previewViewports) as PreviewViewport[]).map(value => (
            <button
              key={value}
              type="button"
              aria-pressed={viewport === value}
              onClick={() => setViewport(value)}
            >
              {previewViewports[value].label}
            </button>
          ))}
        </div>
        <button disabled={starting} onClick={toggle}>
          {starting ? 'Starting…' : preview ? 'Stop preview' : 'Run project preview'}
        </button>
      </header>
      {preview && previewSession ? (
        <div className="canvas">
          <div className="preview-viewport" style={previewViewportStyle(viewport)}>
            <PreviewFrame
              url={previewSession.url}
              sessionId={previewSession.sessionId}
              content={toRuntimeContent(project.contentDocument)}
              selectedInstanceId={selectedInstanceID}
            />
          </div>
        </div>
      ) : (
        <div className="empty">
          {previewError || 'Preview is stopped. Run scripts.dev only for a trusted project; the process runs as your local user. The browser iframe is sandboxed from the Constructor editor.'}
        </div>
      )}
    </main>
  )
}
function RouteEditor() {
  const project=useEditor(state=>state.project)
  const [document,setDocument]=useState<RouteDocument>({schemaVersion:1,id:'development-routes',routes:[]})
  const [revision,setRevision]=useState('')
  const [message,setMessage]=useState('')
  useEffect(()=>{
    if(!project.id)return
    loadRoutes().then(value=>{setDocument(value.document);setRevision(value.revision)}).catch(error=>setMessage(error instanceof Error?error.message:'Routes unavailable'))
  },[project.id,project.pageIDs])
  const update=(index:number,patch:Partial<RouteDocument['routes'][number]>)=>setDocument(current=>({...current,routes:current.routes.map((route,row)=>row===index?{...route,...patch}:route)}))
  const add=()=>{
    const page=project.pageIDs.find(value=>!document.routes.some(route=>route.page===value))
    if(!page){setMessage('Every page already has a route');return}
    setDocument(current=>({...current,routes:[...current.routes,{id:page,path:page==='home'?'/':`/${page}`,page,chunk:'lazy',lazy:true}]}))
    setMessage('Unsaved route')
  }
  const save=async()=>{
    try{const next=await saveRoutes(document,revision);setRevision(next);setMessage('Saved')}
    catch(error){setMessage(error instanceof Error?error.message:'Route validation failed')}
  }
  return <section className="route-editor">
    <header><b>React routes</b><button onClick={add}>Add page route</button><button onClick={save}>Save routes</button></header>
    {document.routes.map((route,index)=><div className="route-card" key={`${route.id}-${index}`}>
      <div className="route-row">
        <code>{route.id}</code>
        <input aria-label={`${route.page} path`} value={route.path} onChange={event=>update(index,{path:event.target.value})}/>
        <select aria-label="Route page" value={route.page} onChange={event=>update(index,{page:event.target.value})}>{project.pageIDs.map(id=><option key={id} value={id}>{pageName(project,id)}</option>)}</select>
        <button onClick={()=>setDocument(current=>({...current,routes:current.routes.filter((_,row)=>row!==index)}))}>Remove</button>
      </div>
      <div className="route-row">
        <input aria-label="Layout chain" placeholder="layout IDs, comma separated" value={(route.layouts??(route.layout?[route.layout]:[])).join(', ')} onChange={event=>update(index,{layouts:event.target.value.split(',').map(value=>value.trim()).filter(Boolean),layout:undefined})}/>
        <input aria-label="Access policy reference" placeholder="access policy ID" value={route.access??''} onChange={event=>update(index,{access:event.target.value||undefined})}/>
        <select aria-label="Chunk policy" value={route.chunk??'same'} onChange={event=>update(index,{chunk:event.target.value as 'same'|'separate'|'lazy'|'preload'})}><option value="same">same chunk</option><option value="separate">separate chunk</option><option value="lazy">lazy</option><option value="preload">preload</option></select>
        <label className="inline-field"><input type="checkbox" checked={route.lazy??false} onChange={event=>update(index,{lazy:event.target.checked})}/> Lazy</label>
      </div>
      <div className="route-row">
        <input aria-label="Preload route IDs" placeholder="preload route ids, comma separated" value={(route.preload??[]).join(', ')} onChange={event=>update(index,{preload:event.target.value.split(',').map(value=>value.trim()).filter(Boolean)})}/>
        <input aria-label="Route metadata JSON" placeholder="metadata JSON" value={JSON.stringify(route.metadata??{})} onChange={event=>{try{update(index,{metadata:JSON.parse(event.target.value) as Record<string,string>})}catch{setMessage('Metadata must be valid JSON')}}}/>
      </div>
    </div>)}
    <small>{message}</small>
  </section>
}
function CaddyfileEditor() {
  const projectID=useEditor(state=>state.project.id)
  const [view,setView]=useState<{projectID:string;draft:CaddyfileDraft}>({projectID:'',draft:emptyCaddyfileDraft()})
  const operation=useRef(0)
  const drafts=useRef(createCaddyfileDraftCache())
  const draft=caddyfileDraftForProject(projectID,view.projectID,view.draft)
  const setForProject=(targetProjectID:string,next:CaddyfileDraft|((current:CaddyfileDraft)=>CaddyfileDraft))=>setView(current=>{
    if(current.projectID!==targetProjectID)return current
    const updated=typeof next==='function'?next(current.draft):next
    drafts.current.set(targetProjectID,updated)
    return {projectID:targetProjectID,draft:updated}
  })
  useEffect(()=>{
    const token=++operation.current
    if(!projectID){setView({projectID:'',draft:emptyCaddyfileDraft()});return}
    let current=true
    const cached=drafts.current.get(projectID)
    if(cached){setView({projectID,draft:cached});return ()=>{current=false}}
    setView({projectID,draft:emptyCaddyfileDraft()})
    void loadCaddyfile(projectID).then(source=>{if(current&&operation.current===token){const next=drafts.current.get(projectID)??createCaddyfileDraft(source.text,source.revision);drafts.current.set(projectID,next);setView({projectID,draft:next})}}).catch(error=>{if(current&&operation.current===token){const next={...emptyCaddyfileDraft(),status:'error' as const,message:error instanceof Error?error.message:'Caddyfile source unavailable'};drafts.current.set(projectID,next);setView({projectID,draft:next})}})
    return ()=>{current=false}
  },[projectID])
  const save=async()=>{
    if(!projectID||view.projectID!==projectID)return
    const current=draft
    if(current.status==='saving'||current.text===current.savedText)return
    const token=operation.current
    const submittedText=current.text
    setForProject(projectID,startCaddyfileSave(current))
    try{
      const saved=await saveCaddyfile(projectID,submittedText,current.revision)
      if(operation.current===token)setForProject(projectID,latest=>finishCaddyfileSave(latest,submittedText,saved.revision,saved.diagnostics))
    }catch(error){
      const message=error instanceof Error?error.message:'Caddyfile save failed'
      const conflict=error instanceof CaddyfileSaveError&&error.conflict
      const diagnostics=error instanceof CaddyfileSaveError?error.diagnostics:[]
      if(operation.current===token)setForProject(projectID,latest=>failCaddyfileSave(latest,message,conflict,diagnostics))
    }
  }
  return <CaddyfileEditorView draft={draft} disabled={draft.status==='loading'||!projectID||view.projectID!==projectID} onChange={text=>{if(projectID)setForProject(projectID,current=>editCaddyfileDraft(current,text))}} onSave={()=>void save()}/>
}
function ProjectSwitcher({siteId,locale,disabled,onProjectOpened}:{siteId:string;locale:string;disabled:boolean;onProjectOpened:(site:SiteDocument,sites:SiteDocument[],project:Project)=>void}) {
  const [projects,setProjects]=useState<{id:string;name:string}[]>([])
  const [selected,setSelected]=useState('')
  const [id,setId]=useState('')
  const [name,setName]=useState('')
  const [message,setMessage]=useState('')
  const refresh=async()=>{const values=await listProjects();setProjects(values);return values}

  useEffect(()=>{
    void Promise.all([refresh(),activeProject()])
      .then(([values,active])=>{
        setProjects(values.some(project=>project.id===active.id)?values:[...values,active])
        setSelected(active.id)
      })
      .catch(error=>setMessage(error instanceof Error?error.message:'Project unavailable'))
  },[])

  const activate=async(value:string)=>{
    if(disabled)return
    setSelected(value)
    setMessage('Opening…')
    try {
      if(useEditor.getState().preview){await stopPreview();useEditor.getState().togglePreview()}
      await activateProject(value)
      const availableSites=await listSiteDocuments()
      const nextSite=availableSites.find(site=>site.id===siteId)??availableSites[0]
      if(!nextSite)throw new Error('Active project has no Site documents')
      const project=await loadProject(nextSite.id,locale)
      onProjectOpened(nextSite,availableSites,project)
      setMessage('Active')
    } catch(error) {
      setMessage(error instanceof Error?error.message:'Project activation failed')
    }
  }

  const create=async()=>{
    if(disabled)return
    setMessage('Creating…')
    try {
      const created=await createProject(id,name)
      setId('');setName('')
      await refresh()
      await activate(created.id)
      setMessage('Created and active')
    } catch(error) {
      setMessage(error instanceof Error?error.message:'Project creation failed')
    }
  }

  return <div className="project-switcher"><span>Project</span>
    <select disabled={disabled} value={selected} onChange={event=>void activate(event.target.value)}>{projects.map(project=><option key={project.id} value={project.id}>{project.name}</option>)}</select>
    <input disabled={disabled} aria-label="Project id" placeholder="id" value={id} onChange={event=>setId(event.target.value)}/>
    <input disabled={disabled} aria-label="Project name" placeholder="New project" value={name} onChange={event=>setName(event.target.value)}/>
    <button disabled={disabled||!id||!name} onClick={()=>void create()}>Create</button><small>{message}</small>
  </div>
}
function ProblemsPanel() {
  const problems=useEditor(state=>state.problems)
  const sortedProblems=sortProblemsBySeverity(problems)
  const open=(problem:EditorProblem)=>{
    const fieldId=navigateToProblem(problem)
    if(fieldId)window.setTimeout(()=>document.getElementById(fieldId)?.focus(),0)
  }
  return <section className="problems-panel" aria-label="Problems">
    <header><b>Problems</b><span>{problems.length?`${problems.length} issue${problems.length===1?'':'s'}`:'No problems'}</span></header>
    {sortedProblems.map(problem=><button className={`problem-row ${problem.severity}`} key={problem.id} disabled={!problem.instanceId&&!problem.pageId} onClick={()=>open(problem)}>
      <span className="problem-severity">{problem.severity==='error'?'●':'▲'}</span>
      <span className="problem-details"><strong>{problem.code}</strong><span>{problem.message}</span>{problem.path&&<code>{problem.path}</code>}</span>
      {(problem.fieldKey||problem.instanceId)&&<small>{[problem.pageId,problem.instanceId,problem.fieldKey].filter(Boolean).join(' / ')}</small>}
    </button>)}
  </section>
}
export function navigateToProblem(problem:EditorProblem):string|undefined {
  const state=useEditor.getState()
  if(problem.instanceId)state.selectInstance(problem.instanceId)
  else if(problem.pageId){
    if(state.project.pageIDs.includes(problem.pageId))state.select(problem.pageId)
  }
  if(problem.instanceId&&problem.fieldKey)return `inspector-${encodeURIComponent(problem.instanceId)}-${encodeURIComponent(problem.fieldKey)}`
}
function DeliveryPanel() {
  const [snapshots, setSnapshots] = useState<{id:string;status:string;gitCommit:string}[]>([])
  const [builds, setBuilds] = useState<{id:string;status:string;artifactPath?:string;artifactChecksum?:string;error?:string}[]>([])
  const [deployments, setDeployments] = useState<{id:string;status:string;siteId:string;environmentId:string;action?:string}[]>([])
  const [refresh, setRefresh] = useState(0)
  const [message, setMessage] = useState('')

  useEffect(() => {
    loadDeliveryHistory().then(result => {
      setSnapshots(result.snapshots)
      setBuilds(result.builds)
      setDeployments(result.deployments)
      setMessage('')
    }).catch(error => setMessage(error instanceof Error ? error.message : 'Delivery history unavailable'))
  }, [refresh])

  const rollback = async (deployment: {id:string;siteId:string;environmentId:string}) => {
    const target = `${deployment.siteId}/${deployment.environmentId}`
    if (!window.confirm(`Rollback the active deployment for ${target}?`)) return
    try {
      await rollbackDeployment(deployment.id, deployment.siteId, deployment.environmentId)
      setRefresh(value => value + 1)
    } catch (error) {
      setMessage(error instanceof Error ? error.message : 'Rollback failed')
    }
  }

  return <section className="delivery-panel">
    <b>Delivery</b>
    <span>Snapshots: {snapshots.length}</span>
    {snapshots.slice(0, 2).map(snapshot => <span key={snapshot.id}>Snapshot {snapshot.status} · {snapshot.gitCommit.slice(0, 7)}</span>)}
    <span>Builds: {builds.length}</span>
    {builds.slice(0, 2).map(build => <span key={build.id}>Build {build.status}{build.artifactChecksum ? ` · SHA-256 ${build.artifactChecksum.slice(0, 12)}` : build.artifactPath ? ` · ${build.artifactPath}` : ''}{build.error ? ` · ${build.error}` : ''}</span>)}
    <span>Deployments: {deployments.length}</span>
    {deployments.slice(0, 2).map(deployment => <span key={deployment.id}>Deployment {deployment.status}{deployment.action === 'rollback' ? ' · rollback' : ''}{deployment.status === 'active' && <button onClick={() => rollback(deployment)}>Rollback</button>}</span>)}
    {message && <span role="alert">{message}</span>}
  </section>
}
function AdminPanel() { const [data,setData]=useState<{users:{id:string;name:string;system:boolean}[];roles:{id:string;name:string;system:boolean}[];permissions:{key:string}[]}|null>(null); const [name,setName]=useState(''); const [key,setKey]=useState(''); const [message,setMessage]=useState(''); const refresh=()=>loadRBAC().then(setData).catch(error=>setMessage(error instanceof Error?error.message:'RBAC unavailable')); useEffect(()=>{void refresh()},[]); if(!data)return <section className="delivery-panel"><b>Admin</b><span>Loading authorization model…</span></section>; const run=async(operation:()=>Promise<unknown>)=>{try{await operation();setName('');setKey('');setMessage('Saved');void refresh()}catch(error){setMessage(error instanceof Error?error.message:'RBAC mutation failed')}}; return <section className="delivery-panel"><b>Admin / RBAC</b><span>Users: {data.users.map(user=>`${user.name}${user.system?' (system)':''}`).join(', ')}</span><span>Roles: {data.roles.map(role=>role.name).join(', ')||'none'}</span><span>Permissions: {data.permissions.map(permission=>permission.key).join(', ')||'none'}</span><div><input placeholder="name" value={name} onChange={event=>setName(event.target.value)}/><button onClick={()=>run(()=>createUser({id:name,name,system:false}))}>Add user</button><button onClick={()=>run(()=>createRole({id:name,name,system:false}))}>Add role</button></div><div><input placeholder="permission key" value={key} onChange={event=>setKey(event.target.value)}/><button onClick={()=>run(()=>createPermission({key}))}>Add permission</button></div><span>{message}</span></section> }
function AssignmentPanel(){const[data,setData]=useState<{users:{id:string;name:string}[];roles:{id:string;name:string}[];permissions:{key:string}[]}|null>(null);const[userId,setUserId]=useState('');const[roleId,setRoleId]=useState('');const[permissionKey,setPermissionKey]=useState('');const[message,setMessage]=useState('');useEffect(()=>{loadRBAC().then(result=>{setData(result);setUserId(result.users[0]?.id??'');setRoleId(result.roles[0]?.id??'');setPermissionKey(result.permissions[0]?.key??'')}).catch(error=>setMessage(error instanceof Error?error.message:'RBAC unavailable'))},[]);if(!data)return <section className="delivery-panel"><b>Assignments</b><span>Loading…</span></section>;const run=async(operation:()=>Promise<unknown>)=>{try{await operation();setMessage('Saved')}catch(error){setMessage(error instanceof Error?error.message:'Assignment failed')}};return <section className="delivery-panel"><b>Assignments</b><div><select value={userId} onChange={event=>setUserId(event.target.value)}>{data.users.map(user=><option key={user.id} value={user.id}>{user.name}</option>)}</select><select value={roleId} onChange={event=>setRoleId(event.target.value)}>{data.roles.map(role=><option key={role.id} value={role.id}>{role.name}</option>)}</select><button onClick={()=>run(()=>assignRole({userId,roleId}))}>Assign role</button></div><div><select value={roleId} onChange={event=>setRoleId(event.target.value)}>{data.roles.map(role=><option key={role.id} value={role.id}>{role.name}</option>)}</select><select value={permissionKey} onChange={event=>setPermissionKey(event.target.value)}>{data.permissions.map(permission=><option key={permission.key} value={permission.key}>{permission.key}</option>)}</select><button onClick={()=>run(()=>grantPermission({roleId,permissionKey}))}>Grant permission</button></div><span>{message}</span></section>}

type PaletteCommand={id:string;label:string;shortcut?:string;disabled?:boolean;run:()=>void}
function CommandPalette({commands,onClose}:{commands:PaletteCommand[];onClose:()=>void}){
  const [query,setQuery]=useState('')
  const [activeIndex,setActiveIndex]=useState(0)
  const input=useRef<HTMLInputElement>(null)
  const dialog=useRef<HTMLElement>(null)
  const matches=commands.filter(command=>command.label.toLowerCase().includes(query.trim().toLowerCase()))
  useEffect(()=>{
    const previous=document.activeElement
    input.current?.focus()
    return()=>{if(previous instanceof HTMLElement&&previous.isConnected)previous.focus()}
  },[])
  useEffect(()=>setActiveIndex(0),[query])
  const runActive=()=>{const command=matches[activeIndex];if(command&&!command.disabled){command.run();onClose()}}
  return <div className="command-backdrop" onMouseDown={event=>{if(event.target===event.currentTarget)onClose()}}>
    <section ref={dialog} className="command-palette" role="dialog" aria-modal="true" aria-label="Command palette" onKeyDown={event=>{
      if(event.key!=='Tab')return
      const focusable=dialog.current?.querySelectorAll<HTMLElement>('input:not(:disabled),button:not(:disabled),[href],[tabindex]:not([tabindex="-1"])')
      if(!focusable?.length){event.preventDefault();dialog.current?.focus();return}
      const first=focusable[0],last=focusable[focusable.length-1]
      if(event.shiftKey&&document.activeElement===first){event.preventDefault();last.focus()}
      else if(!event.shiftKey&&document.activeElement===last){event.preventDefault();first.focus()}
    }}>
      <input ref={input} aria-label="Search commands" placeholder="Search commands…" value={query} onChange={event=>setQuery(event.target.value)} onKeyDown={event=>{if(event.key==='Escape')onClose();if(event.key==='Enter'){event.preventDefault();runActive()}if(event.key==='ArrowDown'&&matches.length){event.preventDefault();setActiveIndex(index=>(index+1)%matches.length)}if(event.key==='ArrowUp'&&matches.length){event.preventDefault();setActiveIndex(index=>(index+matches.length-1)%matches.length)}}}/>
      <div role="listbox" aria-label="Commands">{matches.map((command,index)=><button key={command.id} role="option" aria-selected={index===activeIndex} disabled={command.disabled} onMouseEnter={()=>setActiveIndex(index)} onClick={()=>{command.run();onClose()}}><span>{command.label}</span>{command.shortcut&&<kbd>{command.shortcut}</kbd>}</button>)}{!matches.length&&<p>No matching commands</p>}</div>
      <small>↑/↓ to navigate · Enter to run · Esc to close</small>
    </section>
  </div>
}

export function App() {
  const {problems} = useEditor(); const currentProject=useEditor(state=>state.project); const contentDirty=useEditor(state=>state.contentDirty); const saveStatus=useEditor(state=>state.saveStatus); const canUndo=useEditor(state=>state.past.length>0); const canRedo=useEditor(state=>state.future.length>0); const [busy, setBusy] = useState(false); const [paletteOpen,setPaletteOpen]=useState(false); const [delivery, setDelivery] = useState('No snapshot'); const [site, setSite] = useState<SiteDocument>({schemaVersion:1,id:'',projectId:'',name:'',pages:[],locales:[]}); const [environment, setEnvironment] = useState<Environment>({id: 'local', name: 'Local', kind: 'development'}); const [sites, setSites] = useState<SiteDocument[]>([]); const [themes,setThemes]=useState<ThemeDocument[]>([]); const [themeDraft,setThemeDraft]=useState<ThemeDocument|null>(null); const [newThemeID,setNewThemeID]=useState(''); const [newThemeName,setNewThemeName]=useState(''); const [newThemeTokenPath,setNewThemeTokenPath]=useState(''); const [newThemeTokenType,setNewThemeTokenType]=useState<ThemeTokenType>('color'); const [environments, setEnvironments] = useState<Environment[]>([]); const [newSiteID,setNewSiteID]=useState(''); const [newSiteName,setNewSiteName]=useState(''); const [renameSiteName,setRenameSiteName]=useState(''); const [newLocale,setNewLocale]=useState(''); const [siteMessage,setSiteMessage]=useState(''); const [siteBusy,setSiteBusy]=useState(false); const [contentConflict,setContentConflict]=useState(false); const [conflictMessage,setConflictMessage]=useState('')
  const selectedThemeID=site.themeId??(themes.some(item=>item.id==='default')?'default':themes.length===1?themes[0].id:'')
  const selectedTheme=themes.find(item=>item.id===selectedThemeID)
  useEffect(()=>{setThemeDraft(selectedTheme?structuredClone(selectedTheme):null)},[selectedTheme?.id,selectedTheme?.revision])
  useEffect(()=>{const onKeyDown=(event:KeyboardEvent)=>{if(event.key==='Escape'&&paletteOpen){setPaletteOpen(false);return}if((event.metaKey||event.ctrlKey)&&event.key.toLowerCase()==='k'){event.preventDefault();setPaletteOpen(open=>!open);return}if(!(event.metaKey||event.ctrlKey))return;const target=event.target;if(target instanceof HTMLElement&&target.closest('input,textarea,select,[contenteditable="true"]'))return;const redo=event.key.toLowerCase()==='y'||(event.key.toLowerCase()==='z'&&event.shiftKey);if(event.key.toLowerCase()!=='z'&&!redo)return;event.preventDefault();const state=useEditor.getState();if(redo)state.redo();else state.undo()};window.addEventListener('keydown',onKeyDown);return()=>window.removeEventListener('keydown',onKeyDown)},[paletteOpen])
  useEffect(() => {const defaultEnvironment={id:'local',name:'Local',kind:'development'};void saveEnvironment(defaultEnvironment).then(saved=>setEnvironments([saved])).catch(()=>setEnvironments([defaultEnvironment]));void listProjectThemes().then(setThemes).catch(error=>fail(error));void listSiteDocuments().then(values=>{setSites(values);const selected=values.find(value=>value.id===site.id)??values[0];if(!selected){fail(new Error('Active project has no Site documents'));return}setSite(selected);return loadProject(selected.id,currentProject.locale).then(project=>useEditor.getState().setProject(project))}).catch(error=>fail(error))}, [])
  const fail = (error: unknown) => {
    const message=error instanceof Error?error.message:'Request failed'
    useEditor.setState(state=>({problems:[...state.problems,{id:`request:${message}`,code:'request.failed',severity:'error',message}] }))
  }
  useEffect(()=>{setContentConflict(false);setConflictMessage('')},[currentProject.id,currentProject.siteId,currentProject.locale])
  const persistContent = async () => {
    const before=useEditor.getState()
    const snapshot=before.project
    const base=before.savedContent
    if(!before.contentDirty)return
    useEditor.setState({saveStatus:'saving'})
    try {
      const revisions=await saveProjectContent(snapshot.contentDocument,base,snapshot.siteId,snapshot.locale,snapshot.contentRevision,snapshot.defaultContentRevision)
      useEditor.getState().markSaved(snapshot.contentDocument,revisions)
      setContentConflict(false);setConflictMessage('')
    } catch(error) {
      useEditor.setState({saveStatus:error instanceof ContentRevisionConflict?'conflict':'error'})
      if(error instanceof ContentRevisionConflict){setContentConflict(true);setConflictMessage('The saved baseline and your draft are retained. Try a field-aware merge against the latest content.')}
      throw error
    }
  }
  const reconcileContent = async()=>{
    const state=useEditor.getState();setConflictMessage('');useEditor.setState({saveStatus:'saving'})
    try{
      const latest=await loadProject(state.project.siteId,state.project.locale)
      const merged=mergeContentDocuments(state.savedContent,state.project.contentDocument,latest.contentDocument)
      if(merged.conflicts.length){useEditor.setState({saveStatus:'conflict'});setConflictMessage(`Both versions changed: ${merged.conflicts.join(', ')}. No file was written; your draft remains available.`);return}
      const revisions=await saveProjectContent(merged.document,latest.contentDocument,state.project.siteId,state.project.locale,latest.contentRevision,latest.defaultContentRevision)
      useEditor.getState().markSaved(merged.document,revisions)
      setContentConflict(false);setConflictMessage('Non-overlapping field changes merged and saved.')
    }catch(error){
      useEditor.setState({saveStatus:error instanceof ContentRevisionConflict?'conflict':'error'})
      setContentConflict(true);setConflictMessage(error instanceof Error?error.message:'Merge failed; your draft remains available.')
    }
  }
  const validateActiveDocument=async()=>{
    const result=await validateProject(site.id,currentProject.locale)
    useEditor.setState({problems:result.diagnostics.map(toEditorProblem)})
    return result.valid
  }
  const build = async () => {
    if(currentProject.locale==='default'){fail(new Error('Default values are an editing scope, not a build locale. Select an enabled locale before building.'));return}
    setBusy(true)
    try {
      if(!await validateActiveDocument())return
      const snapshot=await createSnapshot(site.id,currentProject.locale)
      setDelivery(`Snapshot ${snapshot.status}`)
      const buildResult=await buildSnapshot(snapshot.id)
      setDelivery(`Build ${buildResult.status}`)
      if(buildResult.status!=='succeeded')throw new Error(buildResult.error||'Build failed')
    } catch(error) {fail(error)} finally {setBusy(false)}
  }
  const deploy = async () => {
    if(currentProject.locale==='default'){fail(new Error('Default values are an editing scope, not a deployment locale. Select an enabled locale before deploying.'));return}
    const targetLabel=`${site.name} → ${environment.name} (${environment.domain||environment.id})`
    if(!window.confirm(`Publish the current validated build to ${targetLabel}?`))return
    setBusy(true)
    try {
      if(!await validateActiveDocument())return
      const snapshot=await createSnapshot(site.id,currentProject.locale)
      const buildResult=await buildSnapshot(snapshot.id)
      if(buildResult.status!=='succeeded')throw new Error(buildResult.error||'Build failed')
      const targetState=await deploymentTargetState(site.id,environment.id)
      if(!window.confirm(`Deploy this build to ${site.id}/${environment.id}?`))return
      let confirmedGatewayRevision:string|undefined
      if(targetState.requiresBaselineConfirmation){
        const remoteRevision=targetState.gatewayRevision
        if(!remoteRevision)throw new Error('Gateway returned an invalid baseline state')
        if(!window.confirm(`First Constructor deployment to ${site.id}/${environment.id}. Accept existing Gateway release revision ${remoteRevision} as the compare-and-swap baseline?`))return
        confirmedGatewayRevision=remoteRevision
      }
      const result=await deploySnapshot(snapshot.id,buildResult.id,site.id,environment.id,confirmedGatewayRevision)
      setDelivery(`Deployment ${result.status}`)
    } catch(error) {fail(error)} finally {setBusy(false)}
  }
  const onProjectOpened=(nextSite:SiteDocument,nextSites:SiteDocument[],project:Project)=>{
    setSites(nextSites)
    setSite(nextSite)
    void listProjectThemes().then(setThemes).catch(error=>fail(error))
    setRenameSiteName('');setSiteMessage('')
    useEditor.getState().setProject(project)
  }
  const reloadAfterBranchChange=async()=>{
    const nextSites=await listSiteDocuments()
    const nextThemes=await listProjectThemes()
    if(!nextSites.length)throw new Error('Branch changed, but the selected branch contains no Site documents.')
    const nextSite=nextSites.find(item=>item.id===site.id)??nextSites[0]
    const nextLocale=nextSite.locales.includes(currentProject.locale)?currentProject.locale:nextSite.locales[0]
    if(!nextLocale)throw new Error('Branch changed, but the selected Site has no enabled locales.')
    const nextProject=await loadProject(nextSite.id,nextLocale)
    clearEditorBranchCaches()
    setSites(nextSites);setThemes(nextThemes);setSite(nextSite)
    useEditor.getState().setProject(nextProject)
  }
  const onSiteUpdated=(updated:SiteDocument)=>{
    setSite(updated)
    setSites(current=>current.map(item=>item.id===updated.id?updated:item))
  }
  const createSite=async(event:React.FormEvent)=>{
    event.preventDefault()
    if(!currentProject.manifestRevision||!currentProject.siteRevision){setSiteMessage('Reload the project to obtain current revisions.');return}
    setSiteBusy(true);setSiteMessage('Creating Site…')
    try{
      const result=await createProjectSite({id:newSiteID.trim(),name:newSiteName.trim(),sourceSiteId:site.id,revisions:{manifest:currentProject.manifestRevision,sourceSite:currentProject.siteRevision}})
      const nextProject=await loadProject(result.site.id,currentProject.locale)
      setSites(current=>[...current.filter(item=>item.id!==result.site.id),result.site])
      setSite(result.site)
      useEditor.getState().setProject(nextProject)
      setNewSiteID('');setNewSiteName('');setRenameSiteName('');setSiteMessage('Site created with copied pages/locales and empty content.')
    }catch(error){setSiteMessage(error instanceof Error?error.message:'Site creation failed')}
    finally{setSiteBusy(false)}
  }
  const renameSite=async(event:React.FormEvent)=>{
    event.preventDefault()
    if(!currentProject.siteRevision){setSiteMessage('Reload the project to obtain the current Site revision.');return}
    const name=renameSiteName.trim()
    if(!name)return
    setSiteBusy(true);setSiteMessage('Saving Site name…')
    try{
      const updated={...site,name}
      await saveSiteDocument(updated,currentProject.siteRevision)
      const nextProject=await loadProject(site.id,currentProject.locale)
      setSite(nextProject.siteDocument)
      setSites(current=>current.map(item=>item.id===site.id?nextProject.siteDocument:item))
      useEditor.getState().setProject(nextProject)
      setRenameSiteName('');setSiteMessage('Site name saved.')
    }catch(error){setSiteMessage(error instanceof Error?error.message:'Site rename failed')}
    finally{setSiteBusy(false)}
  }
  const changeEnabledLocales=async(nextLocales:string[])=>{
    const current=useEditor.getState().project
    if(!current.siteRevision){setSiteMessage('Reload the project to obtain the current Site revision.');return}
    if(!nextLocales.length){setSiteMessage('A Site must keep at least one enabled locale.');return}
    setSiteBusy(true);setSiteMessage('Saving enabled locales…')
    try{
      const updated={...site,locales:nextLocales}
      await saveSiteDocument(updated,current.siteRevision)
      const targetLocale=nextLocales.includes(current.locale)?current.locale:nextLocales[0]
      const nextProject=await loadProject(site.id,targetLocale)
      setSite(nextProject.siteDocument)
      setSites(values=>values.map(value=>value.id===site.id?nextProject.siteDocument:value))
      useEditor.getState().setProject(nextProject)
      setSiteMessage(`Enabled locales: ${nextLocales.join(', ')}. Existing disabled-locale files are preserved.`)
      setNewLocale('')
    }catch(error){setSiteMessage(error instanceof Error?error.message:'Locale update failed')}
    finally{setSiteBusy(false)}
  }
  const addEnabledLocale=async(event:React.FormEvent)=>{
    event.preventDefault()
    try{await changeEnabledLocales(enableSiteLocale(site.locales,newLocale))}
    catch(error){setSiteMessage(error instanceof Error?error.message:'Locale could not be enabled')}
  }
  const disableLocale=(locale:string)=>{
    if(!window.confirm(`Disable ${locale} for snapshots and validation? Its content file and any local draft will be preserved.`))return
    try{void changeEnabledLocales(disableSiteLocale(site.locales,locale))}
    catch(error){setSiteMessage(error instanceof Error?error.message:'Locale could not be disabled')}
  }
  const selectSiteTheme=async(themeId:string)=>{
    if(!currentProject.siteRevision)return
    setSiteBusy(true);setSiteMessage('Saving Site theme…')
    try{
      const updated={...site,themeId:themeId||undefined}
      await saveSiteDocument(updated,currentProject.siteRevision)
      const nextProject=await loadProject(site.id,currentProject.locale)
      setSite(nextProject.siteDocument)
      setSites(current=>current.map(item=>item.id===site.id?nextProject.siteDocument:item))
      useEditor.getState().setProject(nextProject)
      setSiteMessage(themeId?`Theme ${themeId} selected.`:'Site theme selection cleared.')
    }catch(error){setSiteMessage(error instanceof Error?error.message:'Theme selection failed')}
    finally{setSiteBusy(false)}
  }
  const saveTheme=async(event:React.FormEvent)=>{
    event.preventDefault()
    if(!themeDraft)return
    setSiteBusy(true);setSiteMessage('Saving Theme…')
    try{
      await saveProjectTheme(themeDraft,themeDraft.revision??'')
      const refreshed=await listProjectThemes()
      setThemes(refreshed)
      setSiteMessage(`Theme ${themeDraft.id} saved.`)
    }catch(error){setSiteMessage(error instanceof Error?error.message:'Theme save failed')}
    finally{setSiteBusy(false)}
  }
  const createTheme=async(event:React.FormEvent)=>{
    event.preventDefault()
    const id=newThemeID.trim();const name=newThemeName.trim()
    if(!id||!name)return
    setSiteBusy(true);setSiteMessage('Creating Theme…')
    try{
      const initial:ThemeDocument={schemaVersion:1,id,name,tokens:{'colors.primary':{type:'color',value:'#2563eb'},'spacing.medium':{type:'length',value:'16px'}},revision:''}
      await saveProjectTheme(initial,'')
      const refreshed=await listProjectThemes()
      setThemes(refreshed);setNewThemeID('');setNewThemeName('');setSiteMessage(`Theme ${id} created. Select it for this Site when ready.`)
    }catch(error){setSiteMessage(error instanceof Error?error.message:'Theme creation failed')}
    finally{setSiteBusy(false)}
  }
  const addThemeToken=()=>{
    const key=newThemeTokenPath.trim()
    if(!themeDraft||!key||themeDraft.tokens[key])return
    const defaults:Record<ThemeTokenType,string|number>={color:'#000000',fontFamily:'sans-serif',fontSize:'1rem',fontWeight:400,lineHeight:1.5,length:'8px',shadow:'none',duration:'150ms',easing:'ease'}
    setThemeDraft({...themeDraft,tokens:{...themeDraft.tokens,[key]:{type:newThemeTokenType,value:defaults[newThemeTokenType]}}})
    setNewThemeTokenPath('')
  }
  const removeThemeToken=(key:string)=>setThemeDraft(current=>{
    if(!current)return current
    const tokens={...current.tokens};delete tokens[key]
    const variants={...current.variants}
    for(const variant of ['light','dark'] as const){
      if(!variants[variant])continue
      const overrides={...variants[variant]};delete overrides[key]
      if(Object.keys(overrides).length)variants[variant]=overrides;else delete variants[variant]
    }
    return {...current,tokens,variants}
  })
  const updateThemeToken=(key:string,value:string|number)=>setThemeDraft(current=>current?({...current,tokens:{...current.tokens,[key]:{...current.tokens[key],value}}}):current)
  const updateThemeVariant=(variant:'light'|'dark',key:string,value:string|number|undefined)=>setThemeDraft(current=>{
    if(!current)return current
    const variants={...current.variants};const overrides={...(variants[variant]??{})}
    if(value===undefined)delete overrides[key];else overrides[key]=value
    if(Object.keys(overrides).length)variants[variant]=overrides;else delete variants[variant]
    return {...current,variants}
  })
  const switchingDisabled=busy||saveStatus==='saving'
  const commands:PaletteCommand[]=[
    {id:'save',label:'Save content',shortcut:'⌘/Ctrl+S',disabled:busy||!contentDirty,run:()=>void persistContent().catch(fail)},
    {id:'build',label:'Build project',shortcut:'⌘/Ctrl+B',disabled:busy,run:()=>void (async()=>{try{await persistContent();await build()}catch(error){fail(error)}})()},
    {id:'deploy',label:'Deploy project',disabled:busy,run:()=>void (async()=>{try{await persistContent();await deploy()}catch(error){fail(error)}})()},
    {id:'undo',label:'Undo content change',shortcut:'⌘/Ctrl+Z',disabled:!canUndo||busy,run:()=>useEditor.getState().undo()},
    {id:'redo',label:'Redo content change',shortcut:'⌘/Ctrl+Shift+Z',disabled:!canRedo||busy,run:()=>useEditor.getState().redo()},
  ]
  return <div className="app">
    <Explorer onSiteUpdated={onSiteUpdated}/>
    <div className="center">
      <nav>
        <b>⌘ Constructor</b><span>Development Mode</span><button className="palette-trigger" onClick={()=>setPaletteOpen(true)} aria-keyshortcuts="Meta+K Control+K">⌕ Commands <kbd>⌘K</kbd></button>
        <label>Site <select disabled={switchingDisabled||siteBusy} value={site.id} onChange={event=>{
          const next=sites.find(item=>item.id===event.target.value)
          if(!next)return
          setSite(next)
          setRenameSiteName('');setSiteMessage('')
          void loadProject(next.id,currentProject.locale).then(project=>useEditor.getState().setProject(project)).catch(fail)
        }}>{sites.map(item=><option key={item.id} value={item.id}>{item.name}{hasDraft(currentProject.id,item.id,currentProject.locale)?' · draft':''}</option>)}</select></label>
        <details className="page-create"><summary>New Site</summary><form onSubmit={createSite}>
          <label>Stable ID<input required pattern="[a-z][a-z0-9-]{1,62}" value={newSiteID} onChange={event=>setNewSiteID(event.target.value)} placeholder="campaign"/></label>
          <label>Display name<input required maxLength={160} value={newSiteName} onChange={event=>setNewSiteName(event.target.value)} placeholder="Campaign"/></label>
          <small>Copies pages and enabled locales from the selected Site; content starts empty.</small>
          <button type="submit" disabled={siteBusy||switchingDisabled}>{siteBusy?'Creating…':'Create Site'}</button>
        </form></details>
        <details className="page-create"><summary>Rename Site</summary><form onSubmit={renameSite}>
          <label>Display name<input required maxLength={160} value={renameSiteName} onFocus={()=>{if(!renameSiteName)setRenameSiteName(site.name)}} onChange={event=>setRenameSiteName(event.target.value)}/></label>
          <button type="submit" disabled={siteBusy||switchingDisabled}>{siteBusy?'Saving…':'Save Site name'}</button>
        </form></details>
        <details className="page-create"><summary>Enabled locales</summary>
          <ul>{site.locales.map(locale=><li key={locale}>{locale} <button type="button" aria-label={`Disable locale ${locale}`} disabled={siteBusy||site.locales.length<=1} onClick={()=>disableLocale(locale)}>Disable</button></li>)}</ul>
          <form onSubmit={event=>void addEnabledLocale(event)}>
            <label>Locale<input required pattern="[a-z]{2}(-[A-Z]{2})?" value={newLocale} onChange={event=>setNewLocale(event.target.value)} placeholder="en-US"/></label>
            <small>Format: language or language-region. Disabling a locale never deletes its content document.</small>
            <button type="submit" disabled={siteBusy||switchingDisabled||!newLocale.trim()||site.locales.includes(newLocale.trim())}>Enable locale</button>
          </form>
        </details>
        {siteMessage&&<small role="status">{siteMessage}</small>}
        <label>Locale <select disabled={switchingDisabled} value={currentProject.locale} onChange={event=>{
          void loadProject(site.id,event.target.value).then(project=>useEditor.getState().setProject(project)).catch(fail)
        }}><option value="default">Default values{hasDraft(currentProject.id,site.id,'default')?' · draft':''}</option>{currentProject.locales.map(value=><option key={value} value={value}>{value}{hasDraft(currentProject.id,site.id,value)?' · draft':''}</option>)}</select></label>
        {currentProject.contentFallbackFrom&&<small role="status">Showing fallback content from {currentProject.contentFallbackFrom}; Save will create {currentProject.locale} content.</small>}
        <label>Theme <select disabled={switchingDisabled||siteBusy||themes.length===0} value={site.themeId??(themes.some(item=>item.id==='default')?'default':themes.length===1?themes[0].id:'')} onChange={event=>void selectSiteTheme(event.target.value)}>
          <option value="">No theme selected</option>{themes.map(item=><option key={item.id} value={item.id}>{item.name}</option>)}
        </select></label>
        <details className="page-create"><summary>Theme Editor</summary>
          {themeDraft?<form onSubmit={saveTheme}>
            <b>{themeDraft.name} <small>({themeDraft.id})</small></b>
            {Object.entries(themeDraft.tokens).sort(([left],[right])=>left.localeCompare(right)).map(([key,token])=><fieldset key={key}>
              <legend>{key} · {token.type} <button type="button" aria-label={`Remove token ${key}`} onClick={()=>removeThemeToken(key)}>Remove</button></legend>
              <label>Base value<input type={token.type==='color'&&/^#[0-9a-f]{6}$/i.test(String(token.value))?'color':typeof token.value==='number'?'number':'text'} step={typeof token.value==='number'?'any':undefined} value={String(token.value)} onChange={event=>updateThemeToken(key,typeof token.value==='number'?Number(event.target.value):event.target.value)}/></label>
              {(['light','dark'] as const).map(variant=>{const override=themeDraft.variants?.[variant]?.[key];const enabled=override!==undefined;return <div key={variant}>
                <label><input type="checkbox" checked={enabled} onChange={event=>updateThemeVariant(variant,key,event.target.checked?token.value:undefined)}/> {variant} override</label>
                {enabled&&<label>{variant} value<input type={token.type==='color'&&/^#[0-9a-f]{6}$/i.test(String(override))?'color':typeof override==='number'?'number':'text'} step={typeof override==='number'?'any':undefined} value={String(override)} onChange={event=>updateThemeVariant(variant,key,typeof override==='number'?Number(event.target.value):event.target.value)}/></label>}
              </div>})}
            </fieldset>)}
            <label>New token path<input value={newThemeTokenPath} onChange={event=>setNewThemeTokenPath(event.target.value)} placeholder="colors.accent"/></label>
            <label>Type<select value={newThemeTokenType} onChange={event=>setNewThemeTokenType(event.target.value as ThemeTokenType)}>{(['color','fontFamily','fontSize','fontWeight','lineHeight','length','shadow','duration','easing'] as ThemeTokenType[]).map(type=><option key={type} value={type}>{type}</option>)}</select></label>
            <button type="button" onClick={addThemeToken} disabled={!newThemeTokenPath.trim()||Boolean(themeDraft.tokens[newThemeTokenPath.trim()])}>Add token</button>
            <button type="submit" disabled={siteBusy||switchingDisabled}>{siteBusy?'Saving…':'Save Theme'}</button>
          </form>:<p>Select or create a Theme to edit its tokens.</p>}
        </details>
        <details className="page-create"><summary>Create Theme</summary><form onSubmit={createTheme}>
          <label>Stable ID<input required pattern="[a-z][a-z0-9-]{1,62}" value={newThemeID} onChange={event=>setNewThemeID(event.target.value)} placeholder="brand"/></label>
          <label>Display name<input required maxLength={160} value={newThemeName} onChange={event=>setNewThemeName(event.target.value)} placeholder="Brand"/></label>
          <small>Creates typed starter tokens; save and select it for the current Site separately.</small>
          <button type="submit" disabled={siteBusy||switchingDisabled}>{siteBusy?'Creating…':'Create Theme'}</button>
        </form></details>
        <label>Environment <select disabled={switchingDisabled} value={environment.id} onChange={event=>{
          const next=environments.find(item=>item.id===event.target.value)
          if(next)setEnvironment(next)
        }}>{environments.map(item=><option key={item.id} value={item.id}>{item.name}</option>)}</select></label>
        <span className="spacer"/>
        <button aria-label="Undo content change" title="Undo (⌘/Ctrl+Z)" disabled={!canUndo||busy} onClick={()=>useEditor.getState().undo()}>Undo</button>
        <button aria-label="Redo content change" title="Redo (⌘/Ctrl+Shift+Z)" disabled={!canRedo||busy} onClick={()=>useEditor.getState().redo()}>Redo</button>
        <span aria-live="polite" className={`save-status ${saveStatus}`}>{saveStatus==='saved'?'Saved':saveStatus==='dirty'?'Unsaved changes':saveStatus==='saving'?'Saving…':saveStatus==='conflict'?'Save conflict — draft retained':'Save failed'}</span>
        {contentConflict&&<button disabled={busy||saveStatus==='saving'} onClick={()=>void reconcileContent()}>{saveStatus==='saving'?'Merging…':'Try safe field merge'}</button>}
        <button disabled={busy||!contentDirty} onClick={()=>void persistContent().catch(fail)}>Save</button>
        <button disabled={busy} onClick={async()=>{try{await persistContent();await build()}catch(error){fail(error)}}}>{busy?'Working…':'Build'}</button>
        <button disabled={busy} onClick={async()=>{try{await persistContent();await deploy()}catch(error){fail(error)}}}>Deploy</button>
      </nav>
      <ProjectSwitcher siteId={site.id} locale={currentProject.locale} disabled={switchingDisabled} onProjectOpened={onProjectOpened}/>
      <Canvas/><RouteEditor/><CaddyfileEditor/><GitPanel projectID={currentProject.id} contentDirty={contentDirty} onBranchChanged={reloadAfterBranchChange}/><ProblemsPanel/><DeliveryPanel/><AdminPanel/><AssignmentPanel/><PluginAdminPanel/>
      <footer>{problems.length?`⚠ ${problems.length} problem${problems.length===1?'':'s'}`:'✓ No problems'} <span>{delivery} · API v1</span></footer>
      {conflictMessage&&<div className="conflict-message" role="status">{conflictMessage}</div>}
    </div>
    <Inspector/>
    {paletteOpen&&<CommandPalette commands={commands} onClose={()=>setPaletteOpen(false)}/>}
  </div>
}
