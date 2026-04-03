import { useNavigate } from 'react-router-dom'
import { createGateway } from '../api/gateways'
import GatewayForm from '../components/gateways/GatewayForm'
import type { ClawGatewaySpec } from '../api/types'

export default function GatewayCreatePage() {
  const navigate = useNavigate()
  const handleSubmit = async (name: string, spec: ClawGatewaySpec) => {
    await createGateway({ name, spec })
    navigate('/gateways')
  }
  return <GatewayForm onSubmit={handleSubmit} />
}
