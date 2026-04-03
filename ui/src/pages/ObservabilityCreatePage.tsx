import { useNavigate } from 'react-router-dom'
import { createObservability } from '../api/observabilities'
import ObservabilityForm from '../components/observabilities/ObservabilityForm'
import type { ClawObservabilitySpec } from '../api/types'

export default function ObservabilityCreatePage() {
  const navigate = useNavigate()
  const handleSubmit = async (name: string, spec: ClawObservabilitySpec) => {
    await createObservability({ name, spec })
    navigate('/observabilities')
  }
  return <ObservabilityForm onSubmit={handleSubmit} />
}
