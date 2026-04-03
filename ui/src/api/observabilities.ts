import { apiFetch } from './client'
import type { ClawObservability, ClawObservabilitySpec } from './types'

export const listObservabilities = () =>
  apiFetch<ClawObservability[]>('/api/observabilities')

export const getObservability = (name: string) =>
  apiFetch<ClawObservability>(`/api/observabilities/${name}`)

export const createObservability = (data: { name: string; spec: ClawObservabilitySpec }) =>
  apiFetch<ClawObservability>('/api/observabilities', {
    method: 'POST',
    body: JSON.stringify(data),
  })

export const updateObservability = (name: string, spec: ClawObservabilitySpec) =>
  apiFetch<ClawObservability>(`/api/observabilities/${name}`, {
    method: 'PUT',
    body: JSON.stringify({ spec }),
  })

export const deleteObservability = (name: string) =>
  apiFetch<void>(`/api/observabilities/${name}`, { method: 'DELETE' })
