import { useNavigate } from 'react-router-dom'
import { useResourceList } from '../hooks/useResourceList'
import { deleteAgent } from '../api/agents'
import type { ClawAgent } from '../api/types'
import ResourceTable from '../components/shared/ResourceTable'
import type { Column } from '../components/shared/ResourceTable'
import StatusBadge from '../components/shared/StatusBadge'
import LoadingSpinner from '../components/shared/LoadingSpinner'
import ErrorAlert from '../components/shared/ErrorAlert'

const columns: Column<ClawAgent>[] = [
  { key: 'metadata.name', label: 'Name' },
  { key: 'status.phase', label: 'Phase', render: (a) => <StatusBadge phase={a.status?.phase ?? 'Unknown'} /> },
  { key: 'spec.model.provider', label: 'Model', render: (a) => a.spec.model ? `${a.spec.model.provider ?? ''}/${a.spec.model.name ?? ''}` : '-' },
  { key: 'spec.channels', label: 'Channels', render: (a) => String(a.spec.channels?.length ?? 0) },
]

export default function AgentListPage() {
  const { items, loading, error, refresh } = useResourceList<ClawAgent>('/api/agents')
  const navigate = useNavigate()

  if (loading && items.length === 0) return <LoadingSpinner />
  if (error) return <ErrorAlert message={error} />

  return (
    <div>
      <div className="mb-6 flex items-center justify-between">
        <h1 className="text-2xl font-bold text-claw-accent">Agents</h1>
        <button onClick={() => navigate('/agents/new')} className="rounded bg-claw-accent px-4 py-2 text-sm font-medium text-claw-bg hover:bg-claw-accent/80">
          + Create Agent
        </button>
      </div>
      <ResourceTable
        items={items}
        columns={columns}
        onEdit={(a) => navigate(`/agents/${a.metadata.name}/edit`)}
        onDelete={(a) => deleteAgent(a.metadata.name).then(refresh)}
      />
    </div>
  )
}
