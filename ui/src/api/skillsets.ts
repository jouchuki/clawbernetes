import { apiFetch } from './client'
import type { ClawSkillSet, ClawSkillSetSpec } from './types'

export const listSkillSets = () => apiFetch<ClawSkillSet[]>('/api/skillsets')

export const getSkillSet = (name: string) => apiFetch<ClawSkillSet>(`/api/skillsets/${name}`)

export const createSkillSet = (data: { name: string; spec: ClawSkillSetSpec }) =>
  apiFetch<ClawSkillSet>('/api/skillsets', {
    method: 'POST',
    body: JSON.stringify(data),
  })

export const updateSkillSet = (name: string, spec: ClawSkillSetSpec) =>
  apiFetch<ClawSkillSet>(`/api/skillsets/${name}`, {
    method: 'PUT',
    body: JSON.stringify({ spec }),
  })

export const deleteSkillSet = (name: string) =>
  apiFetch<void>(`/api/skillsets/${name}`, { method: 'DELETE' })
