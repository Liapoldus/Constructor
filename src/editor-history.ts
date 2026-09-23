export type HistoryEntry<Document,Selection> = {
  document:Document
  selection:Selection
  group?:string
  at:number
}

export function recordHistory<Document,Selection>(
  past:HistoryEntry<Document,Selection>[],
  document:Document,
  selection:Selection,
  group?:string,
  at=Date.now(),
  limit=100
) {
  const previous=past.at(-1)
  const coalesce=group!==undefined&&previous?.group===group&&at-previous.at<700
  return coalesce?past:[...past.slice(-(limit-1)),{document,selection,group,at}]
}

export function undoHistory<Document,Selection>(
  document:Document,
  selection:Selection,
  past:HistoryEntry<Document,Selection>[],
  future:HistoryEntry<Document,Selection>[]
) {
  const previous=past.at(-1)
  if(!previous)return null
  const current:HistoryEntry<Document,Selection>={document,selection,at:Date.now()}
  return {document:previous.document,selection:previous.selection,past:past.slice(0,-1),future:[current,...future]}
}

export function redoHistory<Document,Selection>(
  document:Document,
  selection:Selection,
  past:HistoryEntry<Document,Selection>[],
  future:HistoryEntry<Document,Selection>[],
  limit=100
) {
  const next=future[0]
  if(!next)return null
  const current:HistoryEntry<Document,Selection>={document,selection,at:Date.now()}
  return {document:next.document,selection:next.selection,past:[...past,current].slice(-limit),future:future.slice(1)}
}
