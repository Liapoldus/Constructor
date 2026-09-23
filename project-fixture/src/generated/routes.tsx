import React from 'react'
import type { RouteObject } from 'react-router-dom'
import PageHome from '../pages/home.page'
import PageShell from '../layouts/shell.layout'

export type AccessCheck = (policy: string) => boolean
type RouteView = React.ComponentType
const withLayouts = (Page: RouteView, layouts: RouteView[]): RouteView => {
  const Composed = () => layouts.reduceRight<React.ReactNode>((child, Layout) => React.createElement(Layout, null, child), React.createElement(Page))
  return Composed
}
const withAccess = (canAccess: AccessCheck, policy: string | undefined, Page: RouteView, layouts: RouteView[]): RouteView => {
  const View = withLayouts(Page, layouts)
  if (!policy) return View
  return () => canAccess(policy) ? React.createElement(View) : React.createElement('main', { role: 'alert', 'data-access-denied': policy }, 'Access denied')
}

export const createRoutes = (canAccess: AccessCheck): RouteObject[] => [
  { path: '/', Component: withAccess(canAccess, undefined, PageHome, []), handle: {"access":"","chunk":"same","layouts":[],"metadata":null,"preload":null} },
  { path: '/about', lazy: async () => { const Page = (await import('../pages/about.page')).default; return { Component: withAccess(canAccess, "member", Page, [PageShell]) } }, handle: {"access":"member","chunk":"lazy","layouts":["shell"],"metadata":null,"preload":null} },
]

export const routes: RouteObject[] = createRoutes(() => false)
