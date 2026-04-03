import { apiFetch } from './client'
import type { ClawPolicy, ClawPolicySpec } from './types'

export const listPolicies = () => apiFetch<ClawPolicy[]>('/api/policies')

export const getPolicy = (name: string) => apiFetch<ClawPolicy>(`/api/policies/${name}`)

export const createPolicy = (data: { name: string; spec: ClawPolicySpec }) =>
  apiFetch<ClawPolicy>('/api/policies', {
    method: 'POST',
    body: JSON.stringify(data),
  })

export const updatePolicy = (name: string, spec: ClawPolicySpec) =>
  apiFetch<ClawPolicy>(`/api/policies/${name}`, {
    method: 'PUT',
    body: JSON.stringify({ spec }),
  })

export const deletePolicy = (name: string) =>
  apiFetch<void>(`/api/policies/${name}`, { method: 'DELETE' })
