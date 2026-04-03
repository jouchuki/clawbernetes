import { useNavigate, useParams } from 'react-router-dom'
import { useResource } from '../hooks/useResource'
import { updateObservability } from '../api/observabilities'
import type { ClawObservability, ClawObservabilitySpec } from '../api/types'
import ObservabilityForm from '../components/observabilities/ObservabilityForm'
import LoadingSpinner from '../components/shared/LoadingSpinner'
import ErrorAlert from '../components/shared/ErrorAlert'

export default function ObservabilityEditPage() {
  const { name } = useParams()
  const { data, loading, error } = useResource<ClawObservability>(`/api/observabilities/${name}`)
  const navigate = useNavigate()

  const handleSubmit = async (_name: string, spec: ClawObservabilitySpec) => {
    await updateObservability(name!, spec)
    navigate('/observabilities')
  }

  if (loading) return <LoadingSpinner />
  if (error) return <ErrorAlert message={error} />

  return <ObservabilityForm initialData={data} onSubmit={handleSubmit} />
}
