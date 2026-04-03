import { apiFetch } from './client'
import type { ClawChannel, ClawChannelSpec } from './types'

export const listChannels = () => apiFetch<ClawChannel[]>('/api/channels')

export const getChannel = (name: string) => apiFetch<ClawChannel>(`/api/channels/${name}`)

export const createChannel = (data: { name: string; spec: ClawChannelSpec }) =>
  apiFetch<ClawChannel>('/api/channels', {
    method: 'POST',
    body: JSON.stringify(data),
  })

export const updateChannel = (name: string, spec: ClawChannelSpec) =>
  apiFetch<ClawChannel>(`/api/channels/${name}`, {
    method: 'PUT',
    body: JSON.stringify({ spec }),
  })

export const deleteChannel = (name: string) =>
  apiFetch<void>(`/api/channels/${name}`, { method: 'DELETE' })
