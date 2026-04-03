import { useNavigate } from 'react-router-dom'
import { useResourceList } from '../hooks/useResourceList'
import { deleteSkillSet } from '../api/skillsets'
import type { ClawSkillSet } from '../api/types'
import ResourceTable from '../components/shared/ResourceTable'
import type { Column } from '../components/shared/ResourceTable'
import LoadingSpinner from '../components/shared/LoadingSpinner'
import ErrorAlert from '../components/shared/ErrorAlert'

const columns: Column<ClawSkillSet>[] = [
  { key: 'metadata.name', label: 'Name' },
  { key: 'spec.skills', label: 'Skills Count', render: (s) => String(s.spec.skills?.length ?? 0) },
]

export default function SkillSetListPage() {
  const { items, loading, error, refresh } = useResourceList<ClawSkillSet>('/api/skillsets')
  const navigate = useNavigate()

  if (loading && items.length === 0) return <LoadingSpinner />
  if (error) return <ErrorAlert message={error} />

  return (
    <div>
      <div className="mb-6 flex items-center justify-between">
        <h1 className="text-2xl font-bold text-claw-accent">Skill Sets</h1>
        <button onClick={() => navigate('/skillsets/new')} className="rounded bg-claw-accent px-4 py-2 text-sm font-medium text-claw-bg hover:bg-claw-accent/80">
          + Create Skill Set
        </button>
      </div>
      <ResourceTable
        items={items}
        columns={columns}
        onEdit={(s) => navigate(`/skillsets/${s.metadata.name}/edit`)}
        onDelete={(s) => deleteSkillSet(s.metadata.name).then(refresh)}
      />
    </div>
  )
}
