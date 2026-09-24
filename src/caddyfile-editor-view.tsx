import type {CaddyfileDraft} from './caddyfile-draft'

export function CaddyfileEditorView({draft,disabled,onChange,onSave}:{draft:CaddyfileDraft;disabled:boolean;onChange:(text:string)=>void;onSave:()=>void}) {
  const saveDisabled=disabled||draft.status==='loading'||draft.status==='saving'||draft.text===draft.savedText
  const status=draft.status==='loading'?'Loading…':draft.status==='saved'?'Saved':draft.status==='dirty'?'Unsaved changes':draft.status==='saving'?'Saving…':draft.status==='conflict'?'Save conflict — draft retained':'Save failed'
  return <section className="caddyfile-editor" aria-label="Caddyfile editor">
    <header><b>Native Caddyfile</b><span>{status}</span><button disabled={saveDisabled} onClick={onSave}>Save source</button></header>
    <p>Редактируется исходный Caddyfile как обычный текст. Локальная адаптация не выполняется; файл не привязан к Group Publish.</p>
    <textarea aria-label="Caddyfile source" spellCheck={false} value={draft.text} disabled={disabled||draft.status==='loading'} onChange={event=>onChange(event.target.value)} />
    {draft.message&&<div role="alert" className="caddyfile-message">{draft.message}</div>}
    {draft.diagnostics.map((diagnostic,index)=><div className={`caddyfile-diagnostic ${diagnostic.severity}`} role="status" key={`${diagnostic.code}-${diagnostic.path??''}-${index}`}><strong>{diagnostic.code}</strong>{diagnostic.path&&<code>{diagnostic.path}</code>}<span>{diagnostic.message}</span></div>)}
  </section>
}
