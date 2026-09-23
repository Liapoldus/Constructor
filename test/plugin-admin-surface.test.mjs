import test from 'node:test'
import assert from 'node:assert/strict'
import {readFileSync} from 'node:fs'
import {parseAdminSurface,unavailableAdminFieldReason,validateAdminInput} from '../src/plugin-admin-surface.ts'

const pluginSurface = JSON.parse(readFileSync(new URL('./fixtures/forms-db-admin-surface.json', import.meta.url), 'utf8'))
const surface = () => ({...structuredClone(pluginSurface),manifestVersion:'1.4.0',surfaceDigest:'a'.repeat(64)})

test('accepts a digest-bound forms-db surface with schema-declared fields and actions', () => {
  assert.deepEqual(parseAdminSurface(surface(), 'forms-db'), surface())
})

test('rejects missing digest, wrong plugin and duplicate page identifiers', () => {
  const missingDigest = surface(); delete missingDigest.surfaceDigest
  assert.throws(() => parseAdminSurface(missingDigest), /invalid or unsupported/)
  assert.throws(() => parseAdminSurface(surface(), 'another-plugin'), /invalid or unsupported/)
  const duplicate = surface(); duplicate.pages.push(structuredClone(duplicate.pages[0]))
  assert.throws(() => parseAdminSurface(duplicate), /duplicate page ID/)
})

test('rejects unknown section and field types and executable/URL-like properties', () => {
  const unknownSection = surface(); unknownSection.pages[0].sections[0].kind = 'iframe'
  assert.throws(() => parseAdminSurface(unknownSection), /invalid or unsupported/)
  const unknownField = surface(); unknownField.pages[0].sections[0].fields[0].type = 'javascript'
  assert.throws(() => parseAdminSurface(unknownField), /invalid or unsupported/)
  const url = surface(); url.pages[0].url = 'https://attacker.invalid'
  assert.throws(() => parseAdminSurface(url), /invalid or unsupported/)
  const executable = surface(); executable.pages[0].sections[0].html = '<img onerror=alert(1)>'
  assert.throws(() => parseAdminSurface(executable), /invalid or unsupported/)
})

test('dangerous actions require confirmation copy and duplicate section IDs are rejected', () => {
  const noConfirmation = surface(); delete noConfirmation.pages[0].sections[1].actions[0].confirmation
  assert.throws(() => parseAdminSurface(noConfirmation), /invalid or unsupported/)
  const duplicateSection = surface(); duplicateSection.pages[0].sections.push(structuredClone(duplicateSection.pages[0].sections[0]))
  assert.throws(() => parseAdminSurface(duplicateSection), /invalid or unsupported/)
})

test('validates the documented JSON Schema subset, option capabilities and row bindings',()=>{
  const value=surface()
  const field=value.pages[0].sections[0].fields[0]
  field.optionsSource={capability:'forms.list',inputSchema:{type:'object',properties:{site:{type:'string',minLength:1}},required:['site'],additionalProperties:false},valueField:'id',labelField:'name'}
  const action=value.pages[0].sections[1].actions[0]
  action.inputSchema={type:'object',properties:{recordId:{type:'string',minLength:1,maxLength:64}},required:['recordId'],additionalProperties:false}
  action.rowInput={recordId:'id'}
  assert.deepEqual(parseAdminSurface(value,'forms-db'),value)
  assert.equal(unavailableAdminFieldReason({key:'legacy',type:'select'}),'This choice field has no declared options source.')
  assert.deepEqual(validateAdminInput(action.inputSchema,{recordId:'r1'}),[])
  assert.deepEqual(validateAdminInput(action.inputSchema,{recordId:'',extra:true}),['extra is not declared by the input schema','recordId is required'])
  const untrusted=structuredClone(value);untrusted.pages[0].sections[0].fields[0].optionsSource.capability='unlisted.read'
  assert.throws(()=>parseAdminSurface(untrusted),/invalid or unsupported/)
  const remote=structuredClone(value);remote.pages[0].sections[1].actions[0].inputSchema.$ref='https://schemas.invalid/input.json'
  assert.throws(()=>parseAdminSurface(remote),/invalid or unsupported/)
  const badBinding=structuredClone(value);badBinding.pages[0].sections[1].actions[0].rowInput.recordId='missing'
  assert.throws(()=>parseAdminSurface(badBinding),/invalid or unsupported/)
})
