export type EditorProblem = {
  id: string
  code: string
  severity: string
  message: string
  path?: string
  pageId?: string
  instanceId?: string
  fieldKey?: string
}

const severityRank: Record<string, number> = {
  error: 0,
  warning: 1,
  info: 2,
}

export function sortProblemsBySeverity(problems: readonly EditorProblem[]): EditorProblem[] {
  const seen = new Set<string>()
  return problems
    .filter(problem => {
      if (seen.has(problem.id)) return false
      seen.add(problem.id)
      return true
    })
    .map((problem, index) => ({problem, index}))
    .sort((left, right) => {
      const rank = (severityRank[left.problem.severity] ?? 3) - (severityRank[right.problem.severity] ?? 3)
      return rank || left.index - right.index
    })
    .map(({problem}) => problem)
}
