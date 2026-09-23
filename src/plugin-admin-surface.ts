export const adminFieldTypes = [
  'string', 'number', 'boolean', 'select', 'multiselect', 'secret', 'file',
  'directory', 'duration', 'size', 'code', 'keyValue', 'array', 'object',
] as const

export const adminSectionKinds = ['form', 'table', 'detail', 'metrics', 'log'] as const

export type AdminField = {
  key: string
  type: typeof adminFieldTypes[number]
  label?: string
  description?: string
  required?: boolean
  options?: Array<{label: string; value: string | number | boolean}>
  optionsSource?: AdminOptionsSource
}
export type AdminInputProperty = {
  type: 'string' | 'number' | 'integer' | 'boolean'
  title?: string
  description?: string
  enum?: Array<string | number | boolean>
  minLength?: number
  maxLength?: number
  minimum?: number
  maximum?: number
}
export type AdminInputSchema = {type:'object';properties:Record<string,AdminInputProperty>;required?:string[];additionalProperties:false}
export type AdminOptionsSource = {capability:string;inputSchema:AdminInputSchema;valueField:string;labelField:string}
export type AdminAction = {
  id: string
  title: string
  capability: string
  confirmation?: string
  dangerous?: boolean
  inputSchema?: AdminInputSchema
  rowInput?: Record<string,string>
}
export type AdminSection = {
  id: string
  kind: typeof adminSectionKinds[number]
  title?: string
  dataCapability?: string
  inputSchema?: AdminInputSchema
  fields?: Array<AdminField | string>
  columns?: Array<string | {key: string; label: string}>
  actions?: AdminAction[]
}
export type AdminPage = {
  id: string
  title: string
  capability: string
  permissions?: string[]
  sections: AdminSection[]
}
export type AdminSurface = {
  version: 1
  plugin: string
  manifestVersion: string
  surfaceDigest: string
  requiredCapabilities: string[]
  pages: AdminPage[]
}

const idPattern = /^[a-z][a-z0-9-]{0,62}$/
const dataKeyPattern = /^[A-Za-z][A-Za-z0-9_-]{0,62}$/
const digestPattern = /^(?:sha256:)?[a-f0-9]{64}$/i
const capabilityPattern = /^[a-z][a-z0-9._-]{0,127}$/
const fieldTypes = new Set<string>(adminFieldTypes)
const sectionKinds = new Set<string>(adminSectionKinds)

function record(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === 'object' && !Array.isArray(value)
}
function hasOnlyKeys(value: Record<string, unknown>, allowed: readonly string[]): boolean {
  return Object.keys(value).every(key => allowed.includes(key))
}
function nonEmptyText(value: unknown, max: number): value is string {
  return typeof value === 'string' && value.trim().length > 0 && value.length <= max
}
function optionalText(value: unknown, max: number): boolean {
  return value === undefined || (typeof value === 'string' && value.length <= max)
}
function validCapability(value: unknown): value is string {
  return typeof value === 'string' && capabilityPattern.test(value)
}
function validateOptions(value: unknown): value is NonNullable<AdminField['options']> {
  return Array.isArray(value) && value.length <= 200 && value.every(option =>
    record(option) && hasOnlyKeys(option, ['label', 'value']) &&
    nonEmptyText(option.label, 120) &&
    (typeof option.value === 'string' || typeof option.value === 'boolean' ||
      (typeof option.value === 'number' && Number.isFinite(option.value))))
}
function validateInputSchema(value: unknown): value is AdminInputSchema {
  if (!record(value) || !hasOnlyKeys(value, ['type', 'properties', 'required', 'additionalProperties']) || value.type !== 'object' || value.additionalProperties !== false || !record(value.properties)) return false
  const entries=Object.entries(value.properties)
  if(entries.length>64)return false
  for(const [key,property] of entries){
    if(!dataKeyPattern.test(key)||!record(property)||!hasOnlyKeys(property,['type','title','description','enum','minLength','maxLength','minimum','maximum']))return false
    if(!['string','number','integer','boolean'].includes(String(property.type))||!optionalText(property.title,120)||(property.title!==undefined&&!nonEmptyText(property.title,120))||!optionalText(property.description,1000))return false
    for(const constraint of ['minLength','maxLength'] as const)if(property[constraint]!==undefined&&(!Number.isInteger(property[constraint])||Number(property[constraint])<0||Number(property[constraint])>100000))return false
    for(const constraint of ['minimum','maximum'] as const)if(property[constraint]!==undefined&&!(typeof property[constraint]==='number'&&Number.isFinite(property[constraint])))return false
    if(property.minimum!==undefined&&property.maximum!==undefined&&Number(property.minimum)>Number(property.maximum))return false
    if(property.minLength!==undefined&&property.maxLength!==undefined&&Number(property.minLength)>Number(property.maxLength))return false
    if(property.enum!==undefined&&(!Array.isArray(property.enum)||!property.enum.length||property.enum.length>128||!property.enum.every(item=>typeof item==='string'||typeof item==='boolean'||(typeof item==='number'&&Number.isFinite(item)))))return false
  }
  if(value.required!==undefined&&(!Array.isArray(value.required)||value.required.length>entries.length||!value.required.every(item=>typeof item==='string'&&Object.hasOwn(value.properties as Record<string,unknown>,item))||new Set(value.required).size!==value.required.length))return false
  return true
}
function validateOptionsSource(value: unknown, capabilities:Set<string>):value is AdminOptionsSource {
  return record(value)&&hasOnlyKeys(value,['capability','inputSchema','valueField','labelField'])&&validCapability(value.capability)&&capabilities.has(value.capability)&&validateInputSchema(value.inputSchema)&&typeof value.valueField==='string'&&dataKeyPattern.test(value.valueField)&&typeof value.labelField==='string'&&dataKeyPattern.test(value.labelField)
}
function validateField(value: unknown, capabilities:Set<string>): value is AdminField {
  if (!record(value) || !hasOnlyKeys(value, ['key', 'type', 'label', 'description', 'required', 'options','optionsSource'])) return false
  if (typeof value.key !== 'string' || !dataKeyPattern.test(value.key) || typeof value.type !== 'string' || !fieldTypes.has(value.type)) return false
  if (!optionalText(value.label, 120) || (value.label !== undefined && !nonEmptyText(value.label, 120)) || !optionalText(value.description, 1000)) return false
  if (value.required !== undefined && typeof value.required !== 'boolean') return false
  if (value.options !== undefined && !validateOptions(value.options)) return false
  if (value.optionsSource !== undefined && !validateOptionsSource(value.optionsSource,capabilities)) return false
  if (value.options !== undefined && value.optionsSource !== undefined) return false
  if (value.type !== 'select' && value.type !== 'multiselect' && value.options !== undefined) return false
  if (value.type !== 'select' && value.type !== 'multiselect' && value.optionsSource !== undefined) return false
  return true
}
function validateAction(value: unknown, capabilities:Set<string>): value is AdminAction {
  if(!record(value) || !hasOnlyKeys(value, ['id', 'title', 'capability', 'confirmation', 'dangerous','inputSchema','rowInput']) ||
    typeof value.id !== 'string' || !idPattern.test(value.id) || !nonEmptyText(value.title, 120) ||
    !validCapability(value.capability) || !capabilities.has(value.capability) || !optionalText(value.confirmation, 1000) ||
    (value.dangerous !== undefined && typeof value.dangerous !== 'boolean') ||
    (value.dangerous === true && !nonEmptyText(value.confirmation, 1000)) ||
    (value.inputSchema !== undefined && !validateInputSchema(value.inputSchema)))return false
  if(value.rowInput!==undefined){
    if(!record(value.rowInput)||!Object.entries(value.rowInput).length||Object.entries(value.rowInput).length>64||!Object.entries(value.rowInput).every(([inputKey,rowKey])=>dataKeyPattern.test(inputKey)&&typeof rowKey==='string'&&dataKeyPattern.test(rowKey)&&Boolean(value.inputSchema&&record(value.inputSchema)&&record(value.inputSchema.properties)&&Object.hasOwn(value.inputSchema.properties,inputKey))))return false
  }
  return true
}
function validateSection(value: unknown, capabilities:Set<string>): value is AdminSection {
  if (!record(value) || !hasOnlyKeys(value, ['id', 'kind', 'title', 'dataCapability','inputSchema', 'fields', 'columns', 'actions'])) return false
  if (typeof value.id !== 'string' || !idPattern.test(value.id) || typeof value.kind !== 'string' || !sectionKinds.has(value.kind)) return false
  if (!optionalText(value.title, 120) || (value.dataCapability !== undefined && (!validCapability(value.dataCapability)||!capabilities.has(value.dataCapability))) || (value.inputSchema!==undefined&&!validateInputSchema(value.inputSchema))) return false
  if (value.fields !== undefined && (!Array.isArray(value.fields) || value.fields.length > 128 || !value.fields.every(field=>typeof field==='string'?dataKeyPattern.test(field):validateField(field,capabilities)))) return false
  if (value.columns !== undefined && (!Array.isArray(value.columns) || value.columns.length > 64 || !value.columns.every(column => {
    if (typeof column === 'string') return dataKeyPattern.test(column)
    return record(column) && hasOnlyKeys(column, ['key', 'label']) && typeof column.key === 'string' && dataKeyPattern.test(column.key) && nonEmptyText(column.label, 120)
  }))) return false
  if (value.actions !== undefined && (!Array.isArray(value.actions) || value.actions.length > 64 || !value.actions.every(action=>validateAction(action,capabilities)))) return false
  if (value.kind === 'form' && !Array.isArray(value.fields)) return false
  if (value.kind === 'table' && (!validCapability(value.dataCapability) || !Array.isArray(value.columns))) return false
  if (value.kind !== 'table' && value.actions !== undefined) return false
  if (value.kind === 'table' && Array.isArray(value.actions)) {
    const columns = new Set((value.columns as Array<string|{key:string}>).map(column => typeof column==='string'?column:column.key))
    if (!value.actions.every(action => !record(action) || action.rowInput===undefined || Object.values(action.rowInput as Record<string,string>).every(key=>columns.has(key)))) return false
  }
  return true
}
function validatePage(value: unknown, capabilities:Set<string>): value is AdminPage {
  if (!record(value) || !hasOnlyKeys(value, ['id', 'title', 'capability', 'permissions', 'sections'])) return false
  if (typeof value.id !== 'string' || !idPattern.test(value.id) || !nonEmptyText(value.title, 120) || !validCapability(value.capability) || !capabilities.has(value.capability)) return false
  if (value.permissions !== undefined && (!Array.isArray(value.permissions) || value.permissions.length > 64 || !value.permissions.every(validCapability))) return false
  if (!Array.isArray(value.sections) || value.sections.length > 32 || !value.sections.every(section=>validateSection(section,capabilities))) return false
  const ids = new Set<string>()
  for (const section of value.sections as AdminSection[]) {
    if (ids.has(section.id)) return false
    ids.add(section.id)
  }
  return true
}

/** Validate an untrusted Gateway response before it is rendered. No URLs, HTML,
 * executable payloads, unknown field/section types or undeclared properties pass. */
export function parseAdminSurface(value: unknown, expectedPlugin?: string): AdminSurface {
  if (!record(value) || !hasOnlyKeys(value, ['version', 'plugin', 'manifestVersion', 'surfaceDigest', 'requiredCapabilities', 'pages'])) {
    throw new Error('Gateway returned an invalid Admin Surface envelope')
  }
  if (value.version !== 1 || !nonEmptyText(value.plugin, 63) || !idPattern.test(value.plugin) ||
      (expectedPlugin !== undefined && value.plugin !== expectedPlugin) ||
      !nonEmptyText(value.manifestVersion, 128) || typeof value.surfaceDigest !== 'string' || !digestPattern.test(value.surfaceDigest) ||
      !Array.isArray(value.requiredCapabilities) || value.requiredCapabilities.length > 128 || !value.requiredCapabilities.every(validCapability) ||
      !Array.isArray(value.pages) || value.pages.length > 64 || !value.pages.every(page=>validatePage(page,new Set(value.requiredCapabilities as string[])))) {
    throw new Error('Gateway returned an invalid or unsupported Admin Surface')
  }
  if (new Set(value.requiredCapabilities).size !== value.requiredCapabilities.length) {
    throw new Error('Admin Surface contains duplicate required capabilities')
  }
  const ids = new Set<string>()
  for (const page of value.pages as AdminPage[]) {
    if (ids.has(page.id)) throw new Error(`Admin Surface contains duplicate page ID: ${page.id}`)
    ids.add(page.id)
  }
  return value as AdminSurface
}

export function validateAdminInput(schema:AdminInputSchema,value:unknown):string[] {
  if (!validateInputSchema(schema)) return ['Input schema is invalid']
  if(!record(value))return ['Input must be a JSON object']
  const errors:string[]=[]
  for(const key of Object.keys(value))if(!Object.hasOwn(schema.properties,key))errors.push(`${key} is not declared by the input schema`)
  for(const key of schema.required??[])if(!Object.hasOwn(value,key)||value[key]==='')errors.push(`${key} is required`)
  for(const [key,property] of Object.entries(schema.properties)){
    if(!Object.hasOwn(value,key))continue
    const fieldValue=value[key]
    if(fieldValue===''&&(schema.required??[]).includes(key))continue
    const typeMatches=property.type==='string'?typeof fieldValue==='string':property.type==='boolean'?typeof fieldValue==='boolean':typeof fieldValue==='number'&&Number.isFinite(fieldValue)&&(property.type!=='integer'||Number.isInteger(fieldValue))
    if(!typeMatches){errors.push(`${key} must be ${property.type}`);continue}
    if(property.enum&&!property.enum.some(option=>option===fieldValue))errors.push(`${key} must match one of the declared options`)
    if(typeof fieldValue==='string'){
      if(property.minLength!==undefined&&fieldValue.length<property.minLength)errors.push(`${key} is shorter than allowed`)
      if(property.maxLength!==undefined&&fieldValue.length>property.maxLength)errors.push(`${key} is longer than allowed`)
    }
    if(typeof fieldValue==='number'){
      if(property.minimum!==undefined&&fieldValue<property.minimum)errors.push(`${key} is below the minimum`)
      if(property.maximum!==undefined&&fieldValue>property.maximum)errors.push(`${key} is above the maximum`)
    }
  }
  return errors
}

export function unavailableAdminFieldReason(field:AdminField):string|undefined {
  if((field.type==='select'||field.type==='multiselect')&&!field.options&&!field.optionsSource)return 'This choice field has no declared options source.'
  if(field.type==='file'||field.type==='directory')return 'This field needs a host-owned file capability that is not available.'
  if(field.type==='secret')return 'Secret fields are write-only and cannot be used as query filters.'
  return undefined
}
