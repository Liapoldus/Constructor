import {useEffect, useState, type FormEvent} from 'react'
import {checkoutGitBranch, commitProject, createGitBranch, loadGitBranches, loadGitDiff, loadGitHistory, loadGitStatus, type GitBranch, type GitCommitRecord} from './api'

type GitOverview = {
  files: string[]
  diff: string
  branches: GitBranch[]
  history: GitCommitRecord[]
}

export function GitPanel({projectID, contentDirty, onBranchChanged}: {projectID: string; contentDirty: boolean; onBranchChanged:()=>Promise<void>}) {
  const [overview, setOverview] = useState<GitOverview>({files: [], diff: '', branches: [], history: []})
  const [commitMessage, setCommitMessage] = useState('')
  const [branchName, setBranchName] = useState('')
  const [message, setMessage] = useState('')
  const [busy, setBusy] = useState(false)
  const [refresh, setRefresh] = useState(0)

  useEffect(() => {
    if (!projectID) return
    let active = true
    Promise.all([loadGitStatus(), loadGitDiff(), loadGitBranches(), loadGitHistory()])
      .then(([files, diff, branches, history]) => {
        if (!active) return
        setOverview({files, diff, branches, history})
        setMessage('')
      })
      .catch(error => {
        if (active) setMessage(error instanceof Error ? error.message : 'Git overview unavailable')
      })
    return () => { active = false }
  }, [projectID, refresh])

  const commit = async (event: FormEvent) => {
    event.preventDefault()
    const summary = commitMessage.trim()
    if (!summary || !overview.files.length || contentDirty || busy) return
    setBusy(true)
    setMessage('')
    try {
      const revision = await commitProject(summary)
      setCommitMessage('')
      setMessage(`Committed ${revision.slice(0, 7)}`)
      setRefresh(value => value + 1)
    } catch (error) {
      setMessage(error instanceof Error ? error.message : 'Git commit failed')
    } finally {
      setBusy(false)
    }
  }

  const branchOperation = async (operation:()=>Promise<GitBranch>, success:(branch:GitBranch)=>string) => {
    if (overview.files.length || contentDirty || busy) return false
    setBusy(true)
    setMessage('')
    let branch:GitBranch
    try {
      branch=await operation()
    } catch(error) {
      setMessage(error instanceof Error?error.message:'Branch operation failed')
      setBusy(false)
      return false
    }
    try {
      await onBranchChanged()
      setMessage(success(branch))
      setRefresh(value=>value+1)
    } catch(error) {
      setMessage(`Switched to ${branch.name}, but project reload failed: ${error instanceof Error?error.message:'unknown error'}. Reload the app to synchronize.`)
      setRefresh(value=>value+1)
    } finally {
      setBusy(false)
    }
    return true
  }

  const currentBranch = overview.branches.find(branch => branch.current)?.name
  const fileCount = overview.files.length

  return <section className="git-panel" aria-label="Project Git">
    <header>
      <b>Git</b>
      <span>{currentBranch ?? 'No current branch'}</span>
      <button type="button" disabled={busy} onClick={() => setRefresh(value => value + 1)}>Refresh</button>
    </header>
    <div className="git-summary">
      <strong>{fileCount ? `${fileCount} changed file${fileCount === 1 ? '' : 's'}` : 'Working tree clean'}</strong>
      {fileCount > 0 && <ul>{overview.files.map((file, index) => <li key={`${index}:${file}`}><code>{file}</code></li>)}</ul>}
    </div>
    <details>
      <summary>Diff</summary>
      <pre>{overview.diff || 'No changes to show.'}</pre>
    </details>
    <details>
      <summary>Branches ({overview.branches.length})</summary>
      <ul>{overview.branches.map(branch => <li key={branch.name} aria-current={branch.current ? 'true' : undefined}>
        {branch.current ? '● ' : ''}{branch.name} <code>{branch.commit.slice(0, 7)}</code>
        {!branch.current&&<button type="button" disabled={busy||contentDirty||fileCount>0} onClick={()=>void branchOperation(()=>checkoutGitBranch(branch.name),value=>`Switched to ${value.name}; project reloaded.`)}>Switch</button>}
      </li>)}</ul>
      <form onSubmit={event=>{event.preventDefault();const name=branchName.trim();if(!name)return;void branchOperation(()=>createGitBranch(name),value=>`Created and switched to ${value.name}; project reloaded.`).then(succeeded=>{if(succeeded)setBranchName('')})}}>
        <label htmlFor="git-branch-name">New branch</label>
        <input id="git-branch-name" maxLength={200} value={branchName} onChange={event=>setBranchName(event.target.value)} placeholder="feature/editor"/>
        <button type="submit" disabled={busy||contentDirty||fileCount>0||!branchName.trim()}>{busy?'Working…':'Create and switch'}</button>
      </form>
      {(fileCount>0||contentDirty)&&<small>Save and commit all changes before creating or switching branches.</small>}
    </details>
    <details>
      <summary>Recent commits ({overview.history.length})</summary>
      <ol>{overview.history.map(commit => <li key={commit.hash}>
        <code>{commit.hash.slice(0, 7)}</code> {commit.message}
        <small>{commit.author} · {commit.date}</small>
      </li>)}</ol>
    </details>
    <form onSubmit={event => void commit(event)}>
      <label htmlFor="git-commit-message">Commit message</label>
      <input id="git-commit-message" maxLength={200} value={commitMessage} onChange={event => setCommitMessage(event.target.value)} placeholder="Describe the saved changes"/>
      <button type="submit" disabled={busy || !fileCount || !commitMessage.trim() || contentDirty}>
        {busy ? 'Committing…' : 'Commit saved changes'}
      </button>
      {contentDirty && <small>Save the current content draft before committing.</small>}
    </form>
    {message && <span role="status">{message}</span>}
  </section>
}
