import {createContext, createElement, useContext, useEffect, useMemo, useRef, useSyncExternalStore, type ImgHTMLAttributes, type ReactNode} from 'react'

export type PrimitiveProps = Record<string, unknown>
export type FieldType = 'text' | 'rich-text' | 'image' | 'icon' | 'file' | 'select' | 'object' | 'array' | 'reference'
export type FieldSchema = {
  key: string
  type: FieldType
  label?: string
  description?: string
  required?: boolean
  nullable?: boolean
  default?: unknown
  localized?: boolean
  allowedValues?: readonly (string | number | boolean)[]
}
export type ComponentSchema<F extends readonly FieldSchema[] = readonly FieldSchema[]> = {
  id: string
  schemaVersion: number
  kind?: 'component' | 'primitive'
  source?: string
  fields: F
  themeTokens?: readonly string[]
}
type NonNullableFieldValue<F extends FieldSchema> =
  F['type'] extends 'text' | 'rich-text' ? string :
  F['type'] extends 'image' | 'icon' | 'file' ? AssetReference :
  F['type'] extends 'array' ? unknown[] :
  F['type'] extends 'object' ? Record<string, unknown> :
  F['type'] extends 'reference' ? EntityReference :
  F['type'] extends 'select' ? F['allowedValues'] extends readonly (infer V)[] ? V : string : unknown
export type FieldValue<F extends FieldSchema> = F['nullable'] extends true ? NonNullableFieldValue<F> | null : NonNullableFieldValue<F>
export type SchemaProps<F extends readonly FieldSchema[]> = {
  [Field in F[number] as Field['required'] extends true ? Field extends {default: unknown} ? never : Field['key'] : never]: FieldValue<Field>
} & {
  [Field in F[number] as Field['required'] extends true ? Field extends {default: unknown} ? Field['key'] : never : Field['key']]?: FieldValue<Field>
}
export type ContentValue = Record<string, unknown>
export type PreviewRuntimeContent = {pages: Record<string, {instances: Record<string, {component: string; fields: ContentValue}>}>}
export type ValidationError = {path: string; message: string}

export function primitive<P extends PrimitiveProps>(render: (props: P) => ReactNode) { return render }
export function component<const F extends readonly FieldSchema[]>(schema: ComponentSchema<F>, render: (props: SchemaProps<F>) => ReactNode) {
  const defaults = Object.fromEntries(schema.fields.filter(field => field.default !== undefined).map(field => [field.key, field.default]))
  const Component = (props: SchemaProps<F>) => {
    const resolved: Record<string, unknown> = {...defaults, ...props}
    for (const field of schema.fields) if (resolved[field.key] === undefined && field.default !== undefined) resolved[field.key] = field.default
    return render(resolved as SchemaProps<F>)
  }
  return Object.assign(Component, {schema})
}
export function content<T = ContentValue>(value: T): T { return value }

export type AssetReference = {kind: 'asset' | 'image' | 'icon' | 'file'; id: string; alt?: string}
export type EntityReference = {kind: 'reference'; type: string; id: string}
export function asset(id: string): AssetReference { return {kind: 'asset', id} }
export function image(id: string, alt = ''): AssetReference { return {kind: 'image', id, alt} }
export function reference(type: string, id: string): EntityReference { return {kind: 'reference', type, id} }

export type Store<T> = {get: () => T; set: (next: T | ((current: T) => T)) => void; subscribe: (listener: () => void) => () => void}
export type ReadonlyStore<T> = {get: () => T; subscribe: (listener: () => void) => () => void}
type Collector = Set<ReadonlyStore<unknown>>
let activeCollector: Collector | undefined

export function defineState<T>(initial: T): Store<T> {
  let value = initial
  const listeners = new Set<() => void>()
  const store: Store<T> = {
    get: () => { activeCollector?.add(store as ReadonlyStore<unknown>); return value },
    set: next => {
      const updated = typeof next === 'function' ? (next as (current: T) => T)(value) : next
      if (Object.is(value, updated)) return
      value = updated
      listeners.forEach(listener => listener())
    },
    subscribe: listener => { listeners.add(listener); return () => listeners.delete(listener) },
  }
  return store
}

export type Computed<T> = ReadonlyStore<T> & (() => T)
export function computed<T>(read: () => T): Computed<T> {
  let initialized = false
  let value: T
  let dependencies = new Map<ReadonlyStore<unknown>, () => void>()
  let dependencyCandidates: Collector = new Set()
  const listeners = new Set<() => void>()
  let evaluating = false

  const refresh = (notify = true): T => {
    if (evaluating) throw new Error('computed() dependency cycle detected')
    evaluating = true
    const nextDependencies: Collector = new Set()
    const previousCollector = activeCollector
    activeCollector = nextDependencies
    let nextValue: T
    try { nextValue = read() } finally { activeCollector = previousCollector; evaluating = false }

    dependencyCandidates = nextDependencies
    syncDependencies()
    const changed = initialized && !Object.is(value, nextValue)
    value = nextValue
    initialized = true
    if (notify && changed && !evaluating) listeners.forEach(listener => listener())
    return value
  }

  const syncDependencies = () => {
    if (listeners.size === 0) return
    for (const [dependency, unsubscribe] of dependencies) {
      if (!dependencyCandidates.has(dependency)) { unsubscribe(); dependencies.delete(dependency) }
    }
    for (const dependency of dependencyCandidates) {
      if (dependencies.has(dependency)) continue
      dependencies.set(dependency, dependency.subscribe(() => { refresh() }))
    }
  }

  const get = () => {
    activeCollector?.add(derived as ReadonlyStore<unknown>)
    return refresh()
  }
  const derived = Object.assign(() => get(), {
    get,
    subscribe(listener: () => void) {
      const firstSubscriber = listeners.size === 0
      listeners.add(listener)
      if (firstSubscriber) refresh(false)
      return () => { listeners.delete(listener); if (listeners.size === 0) { for (const unsubscribe of dependencies.values()) unsubscribe(); dependencies.clear() } }
    },
  }) as Computed<T>
  return derived
}

export function reactive<T>(store: ReadonlyStore<T>): T { return useSyncExternalStore(store.subscribe, store.get, store.get) }
export function action<Args extends unknown[], Result>(run: (...args: Args) => Result): (...args: Args) => Result { return (...args) => run(...args) }

export type ImageVariant = {src: string; mimeType: string; width: number}
export type GeneratedAsset = {type: string; src: string; mimeType?: string; size?: number; sha256?: string; width?: number; height?: number; variants?: readonly ImageVariant[]}
export type Runtime = {route: (path: string) => string; navigate: (path: string) => void; theme: (name?: string) => string; locale: () => string; asset: (id: string) => GeneratedAsset | undefined}
export type GeneratedArtifacts = {theme?: string; locale?: string; routes?: Record<string, string>; assets?: Record<string, GeneratedAsset>}
export type RuntimeOptions = {artifacts?: GeneratedArtifacts; onNavigate?: (path: string) => void}
const RuntimeContext = createContext<Runtime | null>(null)
export function RuntimeProvider({runtime, children}: {runtime: Runtime; children: ReactNode}) { return createElement(RuntimeContext.Provider, {value: runtime}, children) }
export function useRuntime() { const runtime = useContext(RuntimeContext); if (!runtime) throw new Error('RuntimeProvider is required'); return runtime }

export type ContentImageProps = Omit<ImgHTMLAttributes<HTMLImageElement>, 'src' | 'srcSet' | 'sizes' | 'alt'> & {
  asset: AssetReference | string
  alt?: string
  sizes?: string
}

/** Renders generated responsive variants with the source image as a browser fallback. */
export function ContentImage({asset: reference, alt, sizes, ...imageProps}: ContentImageProps) {
  const runtime = useRuntime()
  const assetId = typeof reference === 'string' ? reference : reference.id
  const metadata = runtime.asset(assetId)
  const resolvedAlt = alt ?? (typeof reference === 'string' ? '' : reference.alt ?? '')
  if (!metadata?.src) {
    return createElement('span', {
      className: imageProps.className,
      id: imageProps.id,
      style: imageProps.style,
      title: imageProps.title,
      role: 'img',
      'aria-label': resolvedAlt || `Missing image ${assetId}`,
      'data-asset-missing': assetId,
    }, resolvedAlt || assetId)
  }

  const variantsByType = new Map<string, ImageVariant[]>()
  for (const variant of metadata.variants ?? []) {
    if (!variant.src || !variant.mimeType || !Number.isSafeInteger(variant.width) || variant.width <= 0) continue
    const variants = variantsByType.get(variant.mimeType) ?? []
    variants.push(variant)
    variantsByType.set(variant.mimeType, variants)
  }
  const sources = [...variantsByType.entries()].map(([mimeType, variants]) => {
    const srcSet = [...variants].sort((left, right) => left.width - right.width).map(variant => `${variant.src} ${variant.width}w`).join(', ')
    return createElement('source', {key: mimeType, type: mimeType, srcSet, ...(sizes ? {sizes} : {})})
  })
  const image = createElement('img', {
    ...imageProps,
    key: 'content-image',
    src: metadata.src,
    alt: resolvedAlt,
    ...(metadata.width ? {width: imageProps.width ?? metadata.width} : {}),
    ...(metadata.height ? {height: imageProps.height ?? metadata.height} : {}),
    loading: imageProps.loading ?? 'lazy',
    decoding: imageProps.decoding ?? 'async',
  })
  return createElement('picture', null, [...sources, image])
}

export type PreviewDraftMessage = {protocol: 1; source: 'liapoldus.constructor'; type: 'content-draft'; sessionId: string; revision: number; content: PreviewRuntimeContent; selectedInstanceId?: string | null}
export type PreviewReadyMessage = {protocol: 1; source: 'liapoldus.constructor'; type: 'preview-ready' | 'preview-ready-ack'; sessionId: string}
export type PreviewMessageTarget = Pick<Window, 'addEventListener' | 'removeEventListener'>
export type PreviewDraftStore = ReadonlyStore<PreviewRuntimeContent> & {
  connect: (target: PreviewMessageTarget, parent: WindowProxy) => () => void
  getSelectedInstanceId: () => string | null
  subscribeSelection: (listener: () => void) => () => void
}
export function createPreviewDraftStore(sessionId: string, initialContent: PreviewRuntimeContent = {pages:{}}): PreviewDraftStore {
  let content = initialContent
  let selectedInstanceId: string | null = null
  let revision = -1
  const listeners = new Set<() => void>()
  const selectionListeners = new Set<() => void>()
  return {
    get: () => content,
    subscribe: listener => { listeners.add(listener); return () => listeners.delete(listener) },
    getSelectedInstanceId: () => selectedInstanceId,
    subscribeSelection: listener => { selectionListeners.add(listener); return () => selectionListeners.delete(listener) },
    connect: (target, parent) => {
      const receive = (event: MessageEvent<unknown>) => {
        // A sandboxed preview without allow-same-origin deliberately reports an
        // opaque "null" origin. Trust it only when the sender is the exact
        // parent window and the per-preview session token below also matches.
        if (event.source !== parent || !isPreviewMessageOrigin(event.origin) || !isPreviewDraftMessage(event.data)) return
        const message = event.data
        if (message.sessionId !== sessionId || message.revision <= revision) return
        let encoded: string
        try { encoded = JSON.stringify(message) } catch { return }
        if (encoded.length > 1_000_000) return
        const nextSelection = message.selectedInstanceId ?? null
        const selectionChanged = selectedInstanceId !== nextSelection
        content = message.content
        selectedInstanceId = nextSelection
        revision = message.revision
        listeners.forEach(listener => listener())
        if (selectionChanged) selectionListeners.forEach(listener => listener())
      }
      target.addEventListener('message', receive)
      return () => target.removeEventListener('message', receive)
    },
  }
}
function isPreviewDraftMessage(value: unknown): value is PreviewDraftMessage {
  if (!value || typeof value !== 'object') return false
  const message = value as Partial<PreviewDraftMessage>
  return message.protocol === 1 && message.source === 'liapoldus.constructor' && message.type === 'content-draft' && typeof message.sessionId === 'string' && Number.isSafeInteger(message.revision) && (message.selectedInstanceId === undefined || message.selectedInstanceId === null || (typeof message.selectedInstanceId === 'string' && message.selectedInstanceId.length > 0 && message.selectedInstanceId.length <= 128)) && isPreviewRuntimeContent(message.content)
}
function isPreviewRuntimeContent(value: unknown): value is PreviewRuntimeContent {
  if (!isRecord(value) || !isRecord(value.pages)) return false
  return Object.values(value.pages).every(page => isRecord(page) && isRecord(page.instances) && Object.values(page.instances).every(instance => isRecord(instance) && typeof instance.component === 'string' && isRecord(instance.fields)))
}
function isRecord(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === 'object' && !Array.isArray(value)
}
function isPreviewMessageOrigin(origin: string) {
  if (origin === 'null') return true
  try {
    const url = new URL(origin)
    if (url.protocol !== 'http:' && url.protocol !== 'https:') return false
    const hostname = url.hostname.toLowerCase().replace(/^\[|\]$/g, '')
    if (hostname === 'localhost' || hostname.endsWith('.localhost')) return true
    return hostname === '::1' || /^127(?:\.\d{1,3}){3}$/.test(hostname)
  } catch { return false }
}
const PreviewDraftContext = createContext<PreviewDraftStore | null>(null)
export function PreviewRuntimeProvider({sessionId, initialContent = {pages:{}}, children}: {sessionId?: string; initialContent?: PreviewRuntimeContent; children: ReactNode}) {
  const resolvedSessionId = sessionId ?? (typeof window === 'undefined' ? '' : new URLSearchParams(window.location.search).get('__liapoldus_preview_session') ?? '')
  const store = useMemo(() => createPreviewDraftStore(resolvedSessionId, initialContent), [resolvedSessionId])
  useEffect(() => {
    if (!resolvedSessionId) return
    const disconnect = store.connect(window, window.parent)
    let acknowledged = false
    const onAcknowledged = (event: MessageEvent<unknown>) => {
      if (event.source === window.parent && isPreviewMessageOrigin(event.origin) && isPreviewReadyMessage(event.data, 'preview-ready-ack', resolvedSessionId)) acknowledged = true
    }
    window.addEventListener('message', onAcknowledged)
    const ready: PreviewReadyMessage = {protocol: 1, source: 'liapoldus.constructor', type: 'preview-ready', sessionId: resolvedSessionId}
    window.parent.postMessage(ready, '*')
    const retry = window.setInterval(() => {
      if (acknowledged) { window.clearInterval(retry); return }
      window.parent.postMessage(ready, '*')
    }, 100)
    return () => { window.clearInterval(retry); window.removeEventListener('message', onAcknowledged); disconnect() }
  }, [resolvedSessionId, store])
  return createElement(PreviewDraftContext.Provider, {value: store}, children)
}
function isPreviewReadyMessage(value: unknown, type: PreviewReadyMessage['type'], sessionId: string): value is PreviewReadyMessage {
  if (!isRecord(value)) return false
  return value.protocol === 1 && value.source === 'liapoldus.constructor' && value.type === type && value.sessionId === sessionId
}
export function usePreviewContent<T = unknown>(pageId: string, instanceId: string, key: string, fallback?: T): T {
  const store = useContext(PreviewDraftContext)
  if (!store) throw new Error('PreviewRuntimeProvider is required')
  const content = useSyncExternalStore(store.subscribe, store.get, store.get)
  const fields = content.pages[pageId]?.instances[instanceId]?.fields
  return (fields && key in fields ? fields[key] : fallback) as T
}
export function usePreviewContentObject(pageId: string, instanceId: string): Readonly<ContentValue> {
  const store = useContext(PreviewDraftContext)
  if (!store) throw new Error('PreviewRuntimeProvider is required')
  const content = useSyncExternalStore(store.subscribe, store.get, store.get)
  return content.pages[pageId]?.instances[instanceId]?.fields ?? {}
}
export function usePreviewSelection(): string | null {
  const store = useContext(PreviewDraftContext)
  if (!store) throw new Error('PreviewRuntimeProvider is required')
  return useSyncExternalStore(store.subscribeSelection, store.getSelectedInstanceId, store.getSelectedInstanceId)
}

const noSelection = () => null
const noSelectionSubscription = () => () => {}

/** Marks an instance for a layout-neutral outline in Constructor preview only. */
export function PreviewInstance({instanceId, children}: {instanceId: string; children: ReactNode}) {
  const container = useRef<HTMLDivElement>(null)
  const store = useContext(PreviewDraftContext)
  const subscribeSelection = store?.subscribeSelection ?? noSelectionSubscription
  const getSelectedInstanceId = store?.getSelectedInstanceId ?? noSelection
  const selectedInstanceId = useSyncExternalStore(subscribeSelection, getSelectedInstanceId, getSelectedInstanceId)

  useEffect(() => {
    const element = container.current
    if (!element || selectedInstanceId !== instanceId || typeof document === 'undefined') return

    const outline = document.createElement('div')
    outline.dataset.constructorSelection = instanceId
    outline.setAttribute('aria-hidden', 'true')
    Object.assign(outline.style, {
      position: 'fixed', pointerEvents: 'none', zIndex: '2147483647',
      border: '2px solid #6aa9ff', background: 'rgba(106, 169, 255, 0.08)',
      borderRadius: '2px', boxSizing: 'border-box', display: 'none',
    })
    document.body.appendChild(outline)

    const updateOutline = () => {
      const range = document.createRange()
      range.selectNodeContents(element)
      const rect = range.getBoundingClientRect()
      const visible = rect.width > 0 && rect.height > 0
      outline.style.display = visible ? 'block' : 'none'
      if (!visible) return
      outline.style.left = `${rect.left}px`
      outline.style.top = `${rect.top}px`
      outline.style.width = `${rect.width}px`
      outline.style.height = `${rect.height}px`
    }

    const resizeObserver = typeof ResizeObserver === 'undefined' ? undefined : new ResizeObserver(updateOutline)
    const observeChildren = () => {
      resizeObserver?.disconnect()
      if (resizeObserver) {
        resizeObserver.observe(element)
        element.querySelectorAll('*').forEach(child => resizeObserver.observe(child))
      }
      updateOutline()
    }
    const mutationObserver = new MutationObserver(observeChildren)
    mutationObserver.observe(element, {subtree: true, childList: true, characterData: true, attributes: true})
    window.addEventListener('resize', updateOutline)
    window.addEventListener('scroll', updateOutline, true)
    observeChildren()

    return () => {
      mutationObserver.disconnect()
      resizeObserver?.disconnect()
      window.removeEventListener('resize', updateOutline)
      window.removeEventListener('scroll', updateOutline, true)
      outline.remove()
    }
  }, [children, instanceId, selectedInstanceId])

  return createElement('div', {ref: container, style: {display: 'contents'}, 'data-constructor-instance': instanceId}, children)
}
/** Creates the production runtime from generated artifacts only. It never contacts Constructor. */
export function createRuntime(options: RuntimeOptions = {}): Runtime {
  const artifacts = options.artifacts ?? {}
  const routeFor = (path: string) => artifacts.routes?.[path] ?? path
  return {route: routeFor, navigate: path => { options.onNavigate?.(routeFor(path)) }, theme: name => name ?? artifacts.theme ?? 'default', locale: () => artifacts.locale ?? 'ru-RU', asset: id => artifacts.assets?.[id]}
}
export const route = (path: string) => path
export const navigate = (path: string) => path
export const theme = (name = 'default') => name
export const locale = (value = 'ru-RU') => value

export function validate(schema: ComponentSchema, values: ContentValue): {valid: boolean; errors: ValidationError[]} {
  const errors: ValidationError[] = []
  const fields = new Map(schema.fields.map(field => [field.key, field]))
  for (const field of schema.fields) {
    const {key} = field
    const value = values[key] === undefined ? field.default : values[key]
    if (value === undefined) {
      if (field.required) errors.push({path: key, message: `${field.label ?? key} is required`})
      continue
    }
    if (value === null) {
      if (!field.nullable) errors.push({path: key, message: `${key} cannot be null`})
      continue
    }
    if (field.required && value === '') { errors.push({path: key, message: `${field.label ?? key} is required`}); continue }
    if ((field.type === 'text' || field.type === 'rich-text') && typeof value !== 'string') errors.push({path: key, message: `${key} must be text`})
    if (['image', 'icon', 'file'].includes(field.type) && (!isRecord(value) || typeof value.id !== 'string' || value.id.length === 0 || (value.kind !== 'asset' && value.kind !== field.type))) errors.push({path: key, message: `${key} must be an ${field.type} asset reference`})
    if (field.type === 'reference' && (!isRecord(value) || value.kind !== 'reference' || typeof value.id !== 'string' || value.id.length === 0 || typeof value.type !== 'string' || value.type.length === 0)) errors.push({path: key, message: `${key} must be an entity reference`})
    if (field.type === 'array' && !Array.isArray(value)) errors.push({path: key, message: `${key} must be an array`})
    if (field.type === 'object' && !isRecord(value)) errors.push({path: key, message: `${key} must be an object`})
    if (field.type === 'select' && typeof value !== 'string' && typeof value !== 'number' && typeof value !== 'boolean') errors.push({path: key, message: `${key} must be a string, number, or boolean`})
    if (field.type === 'select' && field.allowedValues && !field.allowedValues.some(candidate => Object.is(candidate, value))) errors.push({path: key, message: `${key} has an unsupported value`})
  }
  for (const key of Object.keys(values)) if (!fields.has(key)) errors.push({path: key, message: `${key} is not declared by schema`})
  return {valid: errors.length === 0, errors}
}
