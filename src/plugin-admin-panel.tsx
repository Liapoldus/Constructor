import {useEffect, useState} from 'react'
import {listGatewayPlugins, loadPluginAdminOptions, loadPluginAdminSurface, queryPluginAdmin, runPluginAdminAction, PluginSurfaceChangedError, type GatewayPluginInstance} from './api'
import {unavailableAdminFieldReason, validateAdminInput, type AdminField, type AdminInputSchema, type AdminPage, type AdminSection, type AdminSurface} from './plugin-admin-surface'

type Row = Record<string, unknown>

function initialValues(schema?: AdminInputSchema):Record<string,unknown> {
  return Object.fromEntries(Object.entries(schema?.properties??{}).map(([key,property])=>[
    key, property.type==='boolean'?false:property.enum?.[0]??'',
  ]))
}

function InputFields({schema,value,onChange}:{schema:AdminInputSchema;value:Record<string,unknown>;onChange:(next:Record<string,unknown>)=>void}) {
  return <div className="plugin-admin-fields">{Object.entries(schema.properties).map(([key,property])=>{
    const id=`plugin-admin-${key}`
    const current=value[key]??''
    return <label key={key} htmlFor={id}>{property.title??key}{schema.required?.includes(key)?' *':''}
      {property.enum
        ? <select id={id} required={schema.required?.includes(key)} value={String(current)} onChange={event=>onChange({...value,[key]:property.enum?.find(item=>String(item)===event.target.value)??event.target.value})}>{property.enum.map(option=><option key={String(option)} value={String(option)}>{String(option)}</option>)}</select>
        : property.type==='boolean'
          ? <input id={id} type="checkbox" checked={Boolean(current)} onChange={event=>onChange({...value,[key]:event.target.checked})}/>
          : <input id={id} type={property.type==='number'||property.type==='integer'?'number':'text'} required={schema.required?.includes(key)} minLength={property.minLength} maxLength={property.maxLength} min={property.minimum} max={property.maximum} step={property.type==='integer'?1:property.type==='number'?'any':undefined} value={String(current)} onChange={event=>onChange({...value,[key]:property.type==='number'||property.type==='integer'?event.target.value===''?'':Number(event.target.value):event.target.value})}/>}
      {property.description&&<small>{property.description}</small>}
    </label>
  })}</div>
}

function fieldUnavailable(field:AdminField,plugin:GatewayPluginInstance):string|undefined {
  const base=unavailableAdminFieldReason(field)
  if(base)return base
  if(field.optionsSource&&!plugin.capabilities.includes(field.optionsSource.capability))return `This instance does not grant ${field.optionsSource.capability}.`
  return undefined
}

function AdminSectionView({instance,surface,page,section,plugin}:{instance:string;surface:AdminSurface;page:AdminPage;section:AdminSection;plugin:GatewayPluginInstance}) {
  const [values,setValues]=useState<Record<string,unknown>>(()=>initialValues(section.inputSchema))
  const [result,setResult]=useState<unknown>(null)
  const [message,setMessage]=useState('')
  const [busy,setBusy]=useState(false)
  const [selectedRow,setSelectedRow]=useState<Row|null>(null)
  const [optionInputs,setOptionInputs]=useState<Record<string,Record<string,unknown>>>({})
  const [optionsByField,setOptionsByField]=useState<Record<string,Array<{value:string|number|boolean;label:string}>>>({})

  const runQuery=async(input:Record<string,unknown>)=>{
    if(!section.dataCapability||!plugin.capabilities.includes(section.dataCapability)){
      setMessage(`This instance does not grant ${section.dataCapability??'the declared query capability'}.`);return
    }
    if(!section.inputSchema){setMessage('This section has no declared query input schema; querying is disabled.');return}
    const errors=validateAdminInput(section.inputSchema,input)
    if(errors.length){setMessage(errors.join('; '));return}
    setBusy(true);setMessage('');
    try{
      const response=await queryPluginAdmin(instance,page.id,surface.surfaceDigest,{mode:'data',input,limit:50})
      setResult(response);setMessage('Query complete')
    }catch(error){
      setMessage(error instanceof PluginSurfaceChangedError?'Surface changed. Reload this plugin before continuing.':error instanceof Error?error.message:'Plugin query failed')
    }finally{setBusy(false)}
  }

  const performAction=async(action:NonNullable<AdminSection['actions']>[number])=>{
    if(!action.inputSchema){setMessage('This action has no declared input schema and is disabled.');return}
    if(!plugin.capabilities.includes(action.capability)){setMessage(`This instance does not grant ${action.capability}.`);return}
    const input=initialValues(action.inputSchema)
    for(const [inputKey,rowKey] of Object.entries(action.rowInput??{})){
      if(selectedRow&&Object.hasOwn(selectedRow,rowKey))input[inputKey]=selectedRow[rowKey]
    }
    const errors=validateAdminInput(action.inputSchema,input)
    if(errors.length){setMessage(`Select a compatible row first: ${errors.join('; ')}`);return}
    setBusy(true);setMessage('')
    try{
      const response=await runPluginAdminAction(instance,page.id,surface.surfaceDigest,action,input)
      setResult(response);setMessage(response&&typeof response==='object'&&'cancelled'in response?'Action cancelled':'Action complete')
    }catch(error){setMessage(error instanceof PluginSurfaceChangedError?'Surface changed. Reload this plugin before continuing.':error instanceof Error?error.message:'Plugin action failed')}
    finally{setBusy(false)}
  }

  const rows=Array.isArray((result as {items?:unknown}|null)?.items)?(result as {items:Row[]}).items:[]
  return <section className="plugin-admin-section">
    <header><b>{section.title??section.id}</b><span>{section.kind}</span></header>
    {section.kind==='form'&&<div className="plugin-admin-fields">{(section.fields??[]).map((field,index)=>{
      if(typeof field==='string')return <p className="plugin-admin-unavailable" key={`${field}-${index}`}>{field}: field renderer requires the Gateway ConfigSchema projection.</p>
      const reason=fieldUnavailable(field,plugin)
      if(reason)return <p className="plugin-admin-unavailable" key={field.key}>{field.label??field.key}: {reason}</p>
      return <div className="plugin-admin-field" key={field.key}><label>{field.label??field.key}<span className="muted">Declared {field.type} field; this Surface currently defines no write operation.</span></label>
        {field.optionsSource&&<>
          <InputFields schema={field.optionsSource.inputSchema} value={optionInputs[field.key]??initialValues(field.optionsSource.inputSchema)} onChange={next=>setOptionInputs(current=>({...current,[field.key]:next}))}/>
          <button disabled={busy||!plugin.capabilities.includes(field.optionsSource.capability)} onClick={()=>{
            setBusy(true);setMessage('Loading declared choices…')
            void loadPluginAdminOptions(instance,page.id,surface.surfaceDigest,field,optionInputs[field.key]??initialValues(field.optionsSource!.inputSchema)).then(options=>{setOptionsByField(current=>({...current,[field.key]:options}));setMessage(`${options.length} choices loaded.`)}).catch(error=>setMessage(error instanceof Error?error.message:'Options unavailable')).finally(()=>setBusy(false))
          }}>Load choices</button>
          {optionsByField[field.key]&&<ul>{optionsByField[field.key].map((option,index)=><li key={`${String(option.value)}-${index}`}>{option.label} <code>{String(option.value)}</code></li>)}</ul>}
        </>}
        {field.options&&<ul>{field.options.map((option,index)=><li key={`${String(option.value)}-${index}`}>{option.label} <code>{String(option.value)}</code></li>)}</ul>}
      </div>
    })}</div>}
    {section.kind==='table'&&<>
      {section.inputSchema?<><InputFields schema={section.inputSchema} value={values} onChange={setValues}/><button disabled={busy} onClick={()=>void runQuery(values)}>{busy?'Loading…':'Load data'}</button></>:<p className="plugin-admin-unavailable">Query disabled: section.inputSchema is not declared.</p>}
      {rows.length>0&&<div className="plugin-admin-table-wrap"><table><thead><tr><th>Select</th>{(section.columns??[]).map(column=><th key={typeof column==='string'?column:column.key}>{typeof column==='string'?column:column.label}</th>)}</tr></thead><tbody>{rows.map((row,index)=><tr key={String(row.id??index)} aria-selected={selectedRow===row}><td><button onClick={()=>setSelectedRow(row)}>Select</button></td>{(section.columns??[]).map(column=>{const key=typeof column==='string'?column:column.key;return <td key={key}>{typeof row[key]==='string'||typeof row[key]==='number'||typeof row[key]==='boolean'?String(row[key]):JSON.stringify(row[key]??'')}</td>})}</tr>)}</tbody></table></div>}
      {section.actions?.map(action=>{const reason=!action.inputSchema?'No declared inputSchema':!plugin.capabilities.includes(action.capability)?`Missing ${action.capability}`:undefined;return <button className="danger" key={action.id} disabled={busy||Boolean(reason)} title={reason} onClick={()=>void performAction(action)}>{action.title}{reason?` · unavailable: ${reason}`:''}</button>})}
    </>}
    {['detail','metrics','log'].includes(section.kind)&&<p className="plugin-admin-unavailable">This projection is declared but no compatible Gateway query schema/result is available yet.</p>}
    {message&&<small role="status">{message}</small>}
    {result!==null&&<details><summary>Typed response</summary><pre>{JSON.stringify(result,null,2)}</pre></details>}
  </section>
}

export function PluginAdminPanel() {
  const [plugins,setPlugins]=useState<GatewayPluginInstance[]>([])
  const [instance,setInstance]=useState('')
  const [surface,setSurface]=useState<AdminSurface|null>(null)
  const [pageID,setPageID]=useState('')
  const [message,setMessage]=useState('Loading Gateway plugins…')
  const [busy,setBusy]=useState(false)
  const current=plugins.find(plugin=>plugin.id===instance)
  const page=surface?.pages.find(item=>item.id===pageID)??surface?.pages[0]

  const refreshPlugins=async()=>{
    const values=await listGatewayPlugins()
    setPlugins(values)
    setInstance(currentID=>values.some(item=>item.id===currentID)?currentID:values[0]?.id??'')
    setMessage(values.length?'Select a plugin instance.':'No Gateway plugin instances are installed.')
  }
  useEffect(()=>{void refreshPlugins().catch(error=>setMessage(error instanceof Error?error.message:'Gateway plugin list unavailable'))},[])
  useEffect(()=>{
    if(!instance){setSurface(null);return}
    if(!current||current.state!=='ready'||!current.health){setSurface(null);setMessage('Admin Surface is available only for healthy, ready plugin instances.');return}
    if(!current.capabilities.includes('admin.surface.get')){setSurface(null);setMessage('This instance does not grant admin.surface.get.');return}
    let active=true
    setBusy(true);setSurface(null);setMessage('Loading declared Admin Surface…')
    void loadPluginAdminSurface(instance).then(value=>{
      if(!active)return
      setSurface(value);setPageID(value.pages[0]?.id??'');setMessage(value.pages.length?'Surface loaded.':'This plugin declares no admin pages.')
    }).catch(error=>{if(active)setMessage(error instanceof Error?error.message:'Admin Surface unavailable')}).finally(()=>{if(active)setBusy(false)})
    return()=>{active=false}
  },[instance,current?.state,current?.health,current?.capabilities.join(',')])

  return <section className="plugin-admin-panel">
    <header><b>Plugin Admin Surface</b><button disabled={busy} onClick={()=>void refreshPlugins().catch(error=>setMessage(error instanceof Error?error.message:'Gateway plugin list unavailable'))}>Refresh</button>
      <select aria-label="Plugin instance" value={instance} onChange={event=>setInstance(event.target.value)}>{plugins.map(plugin=><option key={plugin.id} value={plugin.id}>{plugin.id} · {plugin.state}</option>)}</select>
      {surface&&<select aria-label="Plugin admin page" value={page?.id??''} onChange={event=>setPageID(event.target.value)}>{surface.pages.map(item=><option key={item.id} value={item.id}>{item.title}</option>)}</select>}
    </header>
    <small role="status">{message}{current&&!current.health?` · health check failed`:''}</small>
    {current&&surface&&page&&current.state==='ready'&&current.health
      ? <><div className="plugin-admin-meta">{surface.plugin} · manifest {surface.manifestVersion} · digest {surface.surfaceDigest.slice(0,12)}…</div>{current.capabilities.includes(page.capability)?<div className="plugin-admin-sections">{page.sections.map(section=><AdminSectionView key={`${instance}:${page.id}:${section.id}:${surface.surfaceDigest}`} instance={instance} surface={surface} page={page} section={section} plugin={current}/>)}</div>:<p className="plugin-admin-unavailable">Page hidden: this instance does not grant {page.capability}.</p>}</>
      : current&&surface?<p className="plugin-admin-unavailable">Admin UI is hidden unless the plugin instance is healthy and ready.</p>:null}
  </section>
}
