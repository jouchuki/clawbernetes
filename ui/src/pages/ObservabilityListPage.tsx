import { useNavigate } from 'react-router-dom'
import { useResourceList } from '../hooks/useResourceList'
import { deleteObservability } from '../api/observabilities'
import type { ClawObservability } from '../api/types'
import ResourceTable from '../components/shared/ResourceTable'
import type { Column } from '../components/shared/ResourceTable'
import StatusBadge from '../components/shared/StatusBadge'
import LoadingSpinner from '../components/shared/LoadingSpinner'
import ErrorAlert from '../components/shared/ErrorAlert'

const columns: Column<ClawObservability>[] = [
  { key: 'metadata.name', label: 'Name' },
  { key: 'spec.otlpEndpoint', label: 'OTLP Endpoint' },
  { key: 'spec.tempo.enabled', label: 'Tempo', render: (o) => <StatusBadge phase={o.spec.tempo?.enabled ? 'Running' : 'Hibernating'} /> },
  { key: 'spec.grafana.enabled', label: 'Grafana', render: (o) => <StatusBadge phase={o.spec.grafana?.enabled ? 'Running' : 'Hibernating'} /> },
]

export default function ObservabilityListPage() {
  const { items, loading, error, refresh } = useResourceList<ClawObservability>('/api/observabilities')
  const navigate = useNavigate()

  if (loading && items.length === 0) return <LoadingSpinner />
  if (error) return <ErrorAlert message={error} />

  return (
    <div>
      <div className="mb-6 flex items-center justify-between">
        <h1 className="text-2xl font-bold text-claw-accent">Observability</h1>
        <button onClick={() => navigate('/observabilities/new')} className="rounded bg-claw-accent px-4 py-2 text-sm font-medium text-claw-bg hover:bg-claw-accent/80">
          + Create Observability
        </button>
      </div>
      <ResourceTable
        items={items}
        columns={columns}
        onEdit={(o) => navigate(`/observabilities/${o.metadata.name}/edit`)}
        onDelete={(o) => deleteObservability(o.metadata.name).then(refresh)}
      />
    </div>
  )
}
