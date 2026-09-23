import type {ContentDocument,ContentInstance} from './api'

export type ContentMergeResult={document:ContentDocument;conflicts:string[]}
type ValueState={present:boolean;value:unknown}
const canonical=(value:unknown):string=>JSON.stringify(value,(_key,item)=>item&&typeof item==='object'&&!Array.isArray(item)?Object.fromEntries(Object.entries(item).sort(([left],[right])=>left.localeCompare(right))):item)
const same=(left:ValueState,right:ValueState)=>left.present===right.present&&(!left.present||canonical(left.value)===canonical(right.value))

function mergeValue(base:ValueState,local:ValueState,remote:ValueState):{value:ValueState;conflict:boolean}{
  const localChanged=!same(base,local),remoteChanged=!same(base,remote)
  if(localChanged&&remoteChanged&&!same(local,remote))return {value:remote,conflict:true}
  if(localChanged)return {value:local,conflict:false}
  return {value:remote,conflict:false}
}

export function mergeContentDocuments(base:ContentDocument,local:ContentDocument,remote:ContentDocument):ContentMergeResult{
  const baseByID=new Map(base.instances.map(instance=>[instance.id,instance]))
  const localByID=new Map(local.instances.map(instance=>[instance.id,instance]))
  const remoteByID=new Map(remote.instances.map(instance=>[instance.id,instance]))
  const ids=[...new Set([...remote.instances.map(instance=>instance.id),...local.instances.map(instance=>instance.id)])]
  const instances:ContentInstance[]=[]
  const conflicts:string[]=[]
  for(const id of ids){
    const baseInstance=baseByID.get(id),localInstance=localByID.get(id),remoteInstance=remoteByID.get(id)
    const presence=mergeValue({present:true,value:Boolean(baseInstance)},{present:true,value:Boolean(localInstance)},{present:true,value:Boolean(remoteInstance)})
    if(presence.conflict){conflicts.push(`${id}:instance`);continue}
    if(!baseInstance){
      const chosen=localInstance??remoteInstance
      if(!chosen)continue
      if(localInstance&&remoteInstance&&canonical(localInstance)!==canonical(remoteInstance)){conflicts.push(`${id}:instance`);continue}
      instances.push(chosen);continue
    }
    if(!localInstance||!remoteInstance){
      const retained=localInstance??remoteInstance
      if(retained&&canonical(retained)!==canonical(baseInstance)){conflicts.push(`${id}:instance`);continue}
      continue
    }
    const merged:ContentInstance={id,pageId:remoteInstance.pageId,component:remoteInstance.component,fields:{}}
    for(const key of ['pageId','component'] as const){
      const result=mergeValue({present:true,value:baseInstance[key]},{present:true,value:localInstance[key]},{present:true,value:remoteInstance[key]})
      if(result.conflict)conflicts.push(`${id}:${key}`)
      else Object.assign(merged,{[key]:result.value.value})
    }
    const keys=new Set([...Object.keys(baseInstance.fields),...Object.keys(localInstance.fields),...Object.keys(remoteInstance.fields)])
    for(const key of keys){
      const result=mergeValue({present:Object.hasOwn(baseInstance.fields,key),value:baseInstance.fields[key]},{present:Object.hasOwn(localInstance.fields,key),value:localInstance.fields[key]},{present:Object.hasOwn(remoteInstance.fields,key),value:remoteInstance.fields[key]})
      if(result.conflict)conflicts.push(`${id}:${key}`)
      if(result.value.present)merged.fields[key]=result.value.value
    }
    instances.push(merged)
  }
  return {document:{schemaVersion:remote.schemaVersion,id:remote.id,instances},conflicts}
}
