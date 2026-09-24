export type CaddyfileDiagnostic={code:string;severity:string;message:string;path?:string}
export type CaddyfileDraft={text:string;savedText:string;revision:string;status:'loading'|'saved'|'dirty'|'saving'|'conflict'|'error';diagnostics:CaddyfileDiagnostic[];message?:string}
export type CaddyfileDraftCache={get:(projectID:string)=>CaddyfileDraft|undefined;set:(projectID:string,draft:CaddyfileDraft)=>void}

export function createCaddyfileDraftCache():CaddyfileDraftCache {
  const drafts=new Map<string,CaddyfileDraft>()
  return {get:projectID=>drafts.get(projectID),set:(projectID,draft)=>{drafts.set(projectID,draft)}}
}

export function createCaddyfileDraft(text:string,revision:string):CaddyfileDraft {
  return {text,savedText:text,revision,status:'saved',diagnostics:[]}
}

export function emptyCaddyfileDraft():CaddyfileDraft {
  return {text:'',savedText:'',revision:'',status:'loading',diagnostics:[]}
}

export function caddyfileDraftForProject(activeProjectID:string,ownerProjectID:string,draft:CaddyfileDraft):CaddyfileDraft {
  return activeProjectID&&activeProjectID===ownerProjectID?draft:emptyCaddyfileDraft()
}

export function editCaddyfileDraft(draft:CaddyfileDraft,text:string):CaddyfileDraft {
  return {...draft,text,status:text===draft.savedText?'saved':'dirty',diagnostics:[],message:undefined}
}

export function startCaddyfileSave(draft:CaddyfileDraft):CaddyfileDraft {
  if(draft.status==='saving'||draft.text===draft.savedText)return draft
  return {...draft,status:'saving',diagnostics:[],message:undefined}
}

export function finishCaddyfileSave(draft:CaddyfileDraft,submittedText:string,revision:string,diagnostics:CaddyfileDiagnostic[]=[]):CaddyfileDraft {
  const unchanged=draft.text===submittedText
  return {...draft,savedText:submittedText,revision,status:unchanged?'saved':'dirty',diagnostics,message:undefined}
}

export function failCaddyfileSave(draft:CaddyfileDraft,message:string,conflict=false,diagnostics:CaddyfileDiagnostic[]=[]):CaddyfileDraft {
  return {...draft,status:conflict?'conflict':'error',message,diagnostics}
}
