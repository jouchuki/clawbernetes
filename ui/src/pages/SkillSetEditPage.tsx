import { useNavigate, useParams } from 'react-router-dom'
import { useResource } from '../hooks/useResource'
import { updateSkillSet } from '../api/skillsets'
import type { ClawSkillSet, ClawSkillSetSpec } from '../api/types'
import SkillSetForm from '../components/skillsets/SkillSetForm'
import LoadingSpinner from '../components/shared/LoadingSpinner'
import ErrorAlert from '../components/shared/ErrorAlert'

export default function SkillSetEditPage() {
  const { name } = useParams()
  const { data, loading, error } = useResource<ClawSkillSet>(`/api/skillsets/${name}`)
  const navigate = useNavigate()

  const handleSubmit = async (_name: string, spec: ClawSkillSetSpec) => {
    await updateSkillSet(name!, spec)
    navigate('/skillsets')
  }

  if (loading) return <LoadingSpinner />
  if (error) return <ErrorAlert message={error} />

  return <SkillSetForm initialData={data} onSubmit={handleSubmit} />
}
