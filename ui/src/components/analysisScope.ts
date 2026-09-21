import type { AnalysisScopeProjection, AnalysisUnitState, RepoStatus } from '../api'

type SearchIndexPosture = 'whole-repository' | 'focused'

// The wire schema leaves search_index_posture an open string; the UI only
// models the two postures the backend can produce. Unknown values fall back
// to whole-repository, matching the previous undefined default, so an
// unrecognized posture degrades to the widest scope instead of crashing.
function toPosture(value: string | undefined): SearchIndexPosture {
  return value === 'focused' ? 'focused' : 'whole-repository'
}

export function analysisScopeFromRepoStatus(
  repository: RepoStatus,
): AnalysisScopeProjection {
  const unit = repository.analysis_unit
  const analysis_unit: AnalysisUnitState | undefined = unit && {
    ...unit,
    search_index_posture: toPosture(unit.search_index_posture),
  }
  return {
    repository: repository.name,
    commit: repository.indexed_commit_hash,
    scope_posture: toPosture(unit?.search_index_posture),
    analysis_unit,
  }
}
