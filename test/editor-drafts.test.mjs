import assert from 'node:assert/strict'
import test from 'node:test'
import {navigateToProblem, useEditor} from '../src/app.tsx'

function loadedProject({siteId,locale,title,projectId='draft-cache-test-project'}) {
  return {
    id:projectId,siteId,name:'Draft cache test',pageIDs:['home'],sitePages:{home:'Home'},
    components:['Hero'],locales:['ru-RU','en-US'],locale,
    contentDocument:{schemaVersion:1,id:`${siteId}-${locale}`,instances:[{id:'hero-1',pageId:'home',component:'hero',fields:{title}}]},
    contentRevision:`revision-${siteId}-${locale}`,
    schemas:{Hero:{id:'hero',schemaVersion:1,kind:'component',source:'src/components/hero/hero.tsx',fields:[{key:'title',type:'string',label:'Title'}]}}
  }
}

test('dirty drafts are restored independently by project, Site and Locale',()=>{
  const state=useEditor.getState()
  state.setProject(loadedProject({siteId:'site-a',locale:'ru-RU',title:'Русский исходный'}))
  useEditor.getState().setContent('title','Русский draft')
  assert.equal(useEditor.getState().contentDirty,true)

  useEditor.getState().setProject(loadedProject({siteId:'site-a',locale:'en-US',title:'English source'}))
  assert.equal(useEditor.getState().project.contentDocument.instances[0].fields.title,'English source')
  useEditor.getState().setContent('title','English draft')

  useEditor.getState().setProject(loadedProject({siteId:'site-a',locale:'ru-RU',title:'Русский исходный'}))
  assert.equal(useEditor.getState().project.contentDocument.instances[0].fields.title,'Русский draft')
  assert.equal(useEditor.getState().contentDirty,true)

  useEditor.getState().setProject(loadedProject({siteId:'site-b',locale:'ru-RU',title:'Другой Site'}))
  assert.equal(useEditor.getState().project.contentDocument.instances[0].fields.title,'Другой Site')

  useEditor.getState().setProject(loadedProject({siteId:'site-a',locale:'en-US',title:'English source'}))
  assert.equal(useEditor.getState().project.contentDocument.instances[0].fields.title,'English draft')
})

test('editing one selected instance leaves sibling instance data intact',()=>{
  const project=loadedProject({siteId:'site-instance-test',locale:'ru-RU',title:'Hero one',projectId:'instance-isolation-test'})
  project.contentDocument.instances.push({id:'hero-2',pageId:'home',component:'hero',fields:{title:'Hero two'}})
  useEditor.getState().setProject(project)
  useEditor.getState().setContent('title','Edited hero one')
  useEditor.getState().selectInstance('hero-2')
  useEditor.getState().setContent('title','Edited hero two')
  const instances=useEditor.getState().project.contentDocument.instances
  assert.deepEqual(instances.map(instance=>instance.fields.title),['Edited hero one','Edited hero two'])
})

test('page selection uses stable IDs when display names collide',()=>{
  const project=loadedProject({siteId:'duplicate-page-labels',locale:'ru-RU',title:'Home',projectId:'page-identity-test'})
  project.pageIDs=['home','contact']
  project.sitePages={home:'Одинаковое имя',contact:'Одинаковое имя'}
  project.contentDocument.instances.push({id:'hero-contact',pageId:'contact',component:'hero',fields:{title:'Contact'}})
  useEditor.getState().setProject(project)
  useEditor.getState().select('contact')
  assert.equal(useEditor.getState().selectedPageID,'contact')
  useEditor.getState().selectInstance('hero-contact')
  useEditor.getState().addInstance('Hero')
  const added=useEditor.getState().project.contentDocument.instances.at(-1)
  assert.equal(added.pageId,'contact')
})

test('saving moves the baseline forward and removes the inactive draft marker',()=>{
  const project=loadedProject({siteId:'site-saved-test',locale:'ru-RU',title:'Before save',projectId:'saved-baseline-test'})
  useEditor.getState().setProject({...project,contentFallbackFrom:'default'})
  useEditor.getState().setContent('title','Saved value')
  const saved=useEditor.getState().project.contentDocument
  useEditor.getState().markSaved(saved,{contentRevision:'new-revision',defaultContentRevision:'new-default-revision'})
  assert.equal(useEditor.getState().contentDirty,false)
  assert.equal(useEditor.getState().saveStatus,'saved')
  assert.equal(useEditor.getState().project.contentRevision,'new-revision')
  assert.equal(useEditor.getState().project.defaultContentRevision,'new-default-revision')
  assert.equal(useEditor.getState().project.contentFallbackFrom,undefined)
  useEditor.getState().setProject(loadedProject({siteId:'site-saved-test',locale:'ru-RU',title:'Saved value',projectId:'saved-baseline-test'}))
  assert.equal(useEditor.getState().contentDirty,false)
  assert.equal(useEditor.getState().project.contentDocument.instances[0].fields.title,'Saved value')
})

test('validation diagnostics survive Site/Locale selection changes and clear on matching edits',()=>{
  const project=loadedProject({siteId:'site-problems-test',locale:'ru-RU',title:'Valid',projectId:'problem-cache-test'})
  useEditor.getState().setProject(project)
  const problem={id:'required-title',code:'content.required',severity:'error',path:'liapoldus/content/site-problems-test/ru-RU.json',message:'title is required',pageId:'home',instanceId:'hero-1',fieldKey:'title'}
  useEditor.setState({problems:[problem]})
  useEditor.getState().setProject(loadedProject({siteId:'site-problems-test',locale:'en-US',title:'English',projectId:'problem-cache-test'}))
  assert.deepEqual(useEditor.getState().problems,[])
  useEditor.getState().setProject(project)
  assert.deepEqual(useEditor.getState().problems,[problem])
  useEditor.getState().setContent('title','Fixed')
  assert.deepEqual(useEditor.getState().problems,[])
})

test('problem navigation selects the diagnosed instance and returns its field control ID',()=>{
  const project=loadedProject({siteId:'site-problem-navigation',locale:'ru-RU',title:'One',projectId:'problem-navigation-test'})
  project.contentDocument.instances.push({id:'hero-2',pageId:'home',component:'hero',fields:{title:'Two'}})
  useEditor.getState().setProject(project)
  const target=navigateToProblem({id:'required',code:'content.required',severity:'error',message:'title is required',pageId:'home',instanceId:'hero-2',fieldKey:'title'})
  assert.equal(useEditor.getState().selectedInstanceID,'hero-2')
  assert.equal(target,'inspector-hero-2-title')
})

test('undo and redo restore the matching validation diagnostics with the content',()=>{
  const project=loadedProject({siteId:'site-problem-history',locale:'ru-RU',title:'',projectId:'problem-history-test'})
  useEditor.getState().setProject(project)
  const problem={id:'required-title-history',code:'content.required',severity:'error',message:'title is required',pageId:'home',instanceId:'hero-1',fieldKey:'title'}
  useEditor.setState({problems:[problem]})
  useEditor.getState().setContent('title','Now valid')
  assert.deepEqual(useEditor.getState().problems,[])
  useEditor.getState().undo()
  assert.deepEqual(useEditor.getState().problems,[problem])
  useEditor.getState().redo()
  assert.deepEqual(useEditor.getState().problems,[])
})
