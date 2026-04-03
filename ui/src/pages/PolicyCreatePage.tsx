import { useNavigate } from 'react-router-dom'
import { createPolicy } from '../api/policies'
import PolicyForm from '../components/policies/PolicyForm'
import type { ClawPolicySpec } from '../api/types'

export default function PolicyCreatePage() {
  const navigate = useNavigate()
  const handleSubmit = async (name: string, spec: ClawPolicySpec) => {
    await createPolicy({ name, spec })
    navigate('/policies')
  }
  return <PolicyForm onSubmit={handleSubmit} />
}
