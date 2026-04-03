import { useNavigate, useParams } from 'react-router-dom'
import { useResource } from '../hooks/useResource'
import { updateGateway } from '../api/gateways'
import type { ClawGateway, ClawGatewaySpec } from '../api/types'
import GatewayForm from '../components/gateways/GatewayForm'
import LoadingSpinner from '../components/shared/LoadingSpinner'
import ErrorAlert from '../components/shared/ErrorAlert'

export default function GatewayEditPage() {
  const { name } = useParams()
  const { data, loading, error } = useResource<ClawGateway>(`/api/gateways/${name}`)
  const navigate = useNavigate()

  const handleSubmit = async (_name: string, spec: ClawGatewaySpec) => {
    await updateGateway(name!, spec)
    navigate('/gateways')
  }

  if (loading) return <LoadingSpinner />
  if (error) return <ErrorAlert message={error} />

  return <GatewayForm initialData={data} onSubmit={handleSubmit} />
}
