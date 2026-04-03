import { useNavigate } from 'react-router-dom'
import { createSkillSet } from '../api/skillsets'
import SkillSetForm from '../components/skillsets/SkillSetForm'
import type { ClawSkillSetSpec } from '../api/types'

export default function SkillSetCreatePage() {
  const navigate = useNavigate()
  const handleSubmit = async (name: string, spec: ClawSkillSetSpec) => {
    await createSkillSet({ name, spec })
    navigate('/skillsets')
  }
  return <SkillSetForm onSubmit={handleSubmit} />
}
