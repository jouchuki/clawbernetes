import { useNavigate } from 'react-router-dom'
import { createAgent } from '../api/agents'
import AgentForm from '../components/agents/AgentForm'
import type { ClawAgentSpec } from '../api/types'

export default function AgentCreatePage() {
  const navigate = useNavigate()
  const handleSubmit = async (name: string, spec: ClawAgentSpec) => {
    await createAgent({ name, spec })
    navigate('/agents')
  }
  return <AgentForm onSubmit={handleSubmit} />
}
