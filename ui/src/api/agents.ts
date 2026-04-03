import { apiFetch } from './client'
import type { ClawAgent, ClawAgentSpec } from './types'

export const listAgents = () => apiFetch<ClawAgent[]>('/api/agents')

export const getAgent = (name: string) => apiFetch<ClawAgent>(`/api/agents/${name}`)

export const createAgent = (data: { name: string; spec: ClawAgentSpec }) =>
  apiFetch<ClawAgent>('/api/agents', {
    method: 'POST',
    body: JSON.stringify(data),
  })

export const updateAgent = (name: string, spec: ClawAgentSpec) =>
  apiFetch<ClawAgent>(`/api/agents/${name}`, {
    method: 'PUT',
    body: JSON.stringify({ spec }),
  })

export const deleteAgent = (name: string) =>
  apiFetch<void>(`/api/agents/${name}`, { method: 'DELETE' })
