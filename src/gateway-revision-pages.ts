import type {GatewayGroupRevisionPage} from './api'

export type GatewayRevisionView={pages:GatewayGroupRevisionPage[];requestCursors:Array<string|null>;pageIndex:number}
export type GatewayRevisionViewCache=Record<string,GatewayRevisionView>

export function gatewayRevisionViewForGroup(cache:GatewayRevisionViewCache,groupID:string):GatewayRevisionView|undefined {
  return cache[groupID]
}

export function cacheGatewayRevisionView(cache:GatewayRevisionViewCache,groupID:string,view:GatewayRevisionView):GatewayRevisionViewCache {
  return {...cache,[groupID]:view}
}

export function createGatewayRevisionView(page:GatewayGroupRevisionPage):GatewayRevisionView {
  return {pages:[page],requestCursors:[null],pageIndex:0}
}

export function appendGatewayRevisionPage(view:GatewayRevisionView,cursor:string,page:GatewayGroupRevisionPage):GatewayRevisionView {
  const current=view.pages[view.pageIndex]
  if(!current||current.nextCursor!==cursor)throw new Error('Gateway revision cursor does not match the current page')
  if(view.requestCursors.includes(cursor)||page.nextCursor===cursor||page.nextCursor!==null&&view.requestCursors.includes(page.nextCursor))throw new Error('Gateway revision pagination returned a repeated cursor')
  const seen=new Set(view.pages.flatMap(item=>item.items.map(revision=>revision.id)))
  if(page.items.some(revision=>seen.has(revision.id)))throw new Error('Gateway revision pagination returned a duplicate revision')
  const pages=view.pages.slice(0,view.pageIndex+1)
  const requestCursors=view.requestCursors.slice(0,view.pageIndex+1)
  pages.push(page)
  requestCursors.push(cursor)
  return {pages,requestCursors,pageIndex:pages.length-1}
}

export function selectGatewayRevisionPage(view:GatewayRevisionView,pageIndex:number):GatewayGroupRevisionPage {
  const page=view.pages[pageIndex]
  if(!page)throw new Error('Gateway revision page is unavailable')
  return page
}
