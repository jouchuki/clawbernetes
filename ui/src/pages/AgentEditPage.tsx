import { useNavigate, useParams } from 'react-router-dom'
import { useResource } from '../hooks/useResource'
import { updateAgent } from '../api/agents'
import type { ClawAgent, ClawAgentSpec } from '../api/types'
import AgentForm from '../components/agents/AgentForm'
import LoadingSpinner from '../components/shared/LoadingSpinner'
import ErrorAlert from '../components/shared/ErrorAlert'

export default function AgentEditPage() {
  const { name } = useParams()
  const { data, loading, error } = useResource<ClawAgent>(`/api/agents/${name}`)
  const navigate = useNavigate()

  const handleSubmit = async (_name: string, spec: ClawAgentSpec) => {
    await updateAgent(name!, spec)
    navigate('/agents')
  }

  if (loading) return <LoadingSpinner />
  if (error) return <ErrorAlert message={error} />

  return <AgentForm initialData={data} onSubmit={handleSubmit} />
}
