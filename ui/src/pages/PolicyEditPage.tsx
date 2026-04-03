import { useNavigate, useParams } from 'react-router-dom'
import { useResource } from '../hooks/useResource'
import { updatePolicy } from '../api/policies'
import type { ClawPolicy, ClawPolicySpec } from '../api/types'
import PolicyForm from '../components/policies/PolicyForm'
import LoadingSpinner from '../components/shared/LoadingSpinner'
import ErrorAlert from '../components/shared/ErrorAlert'

export default function PolicyEditPage() {
  const { name } = useParams()
  const { data, loading, error } = useResource<ClawPolicy>(`/api/policies/${name}`)
  const navigate = useNavigate()

  const handleSubmit = async (_name: string, spec: ClawPolicySpec) => {
    await updatePolicy(name!, spec)
    navigate('/policies')
  }

  if (loading) return <LoadingSpinner />
  if (error) return <ErrorAlert message={error} />

  return <PolicyForm initialData={data} onSubmit={handleSubmit} />
}
