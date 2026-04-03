import { useNavigate } from 'react-router-dom'
import { useResourceList } from '../hooks/useResourceList'
import { deleteGateway } from '../api/gateways'
import type { ClawGateway } from '../api/types'
import ResourceTable from '../components/shared/ResourceTable'
import type { Column } from '../components/shared/ResourceTable'
import LoadingSpinner from '../components/shared/LoadingSpinner'
import ErrorAlert from '../components/shared/ErrorAlert'

const columns: Column<ClawGateway>[] = [
  { key: 'metadata.name', label: 'Name' },
  { key: 'spec.port', label: 'Port', render: (g) => String(g.spec.port ?? '-') },
  { key: 'spec.topology', label: 'Mode', render: (g) => g.spec.topology ?? '-' },
]

export default function GatewayListPage() {
  const { items, loading, error, refresh } = useResourceList<ClawGateway>('/api/gateways')
  const navigate = useNavigate()

  if (loading && items.length === 0) return <LoadingSpinner />
  if (error) return <ErrorAlert message={error} />

  return (
    <div>
      <div className="mb-6 flex items-center justify-between">
        <h1 className="text-2xl font-bold text-claw-accent">Gateways</h1>
        <button onClick={() => navigate('/gateways/new')} className="rounded bg-claw-accent px-4 py-2 text-sm font-medium text-claw-bg hover:bg-claw-accent/80">
          + Create Gateway
        </button>
      </div>
      <ResourceTable
        items={items}
        columns={columns}
        onEdit={(g) => navigate(`/gateways/${g.metadata.name}/edit`)}
        onDelete={(g) => deleteGateway(g.metadata.name).then(refresh)}
      />
    </div>
  )
}
