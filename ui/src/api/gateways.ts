import { apiFetch } from './client'
import type { ClawGateway, ClawGatewaySpec } from './types'

export const listGateways = () => apiFetch<ClawGateway[]>('/api/gateways')

export const getGateway = (name: string) => apiFetch<ClawGateway>(`/api/gateways/${name}`)

export const createGateway = (data: { name: string; spec: ClawGatewaySpec }) =>
  apiFetch<ClawGateway>('/api/gateways', {
    method: 'POST',
    body: JSON.stringify(data),
  })

export const updateGateway = (name: string, spec: ClawGatewaySpec) =>
  apiFetch<ClawGateway>(`/api/gateways/${name}`, {
    method: 'PUT',
    body: JSON.stringify({ spec }),
  })

export const deleteGateway = (name: string) =>
  apiFetch<void>(`/api/gateways/${name}`, { method: 'DELETE' })
