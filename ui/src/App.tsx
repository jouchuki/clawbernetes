import { useEffect, useState } from 'react'
import './App.css'

const API = import.meta.env.DEV ? 'http://localhost:9090' : ''

interface Agent {
  name: string
  phase: string
  podName?: string
  provider?: string
  model?: string
  soul?: string
  channels?: string[]
  workspacePVC?: string
  workspaceMode?: string
  reclaimPolicy?: string
  a2aEnabled: boolean
  a2aPeers?: string[]
  a2aSkills?: string[]
  budgetDaily?: number
  budgetMonthly?: number
  toolDeny?: string[]
}

interface Channel {
  name: string
  type: string
  enabled: boolean
  config?: Record<string, string>
}

interface Summary {
  totalAgents: number
  runningAgents: number
  totalChannels: number
  a2aConnections: number
}

function phaseColor(phase: string) {
  switch (phase) {
    case 'Running': return '#4ecca3'
    case 'Progressing': return '#ffd369'
    case 'Error': return '#fc5185'
    default: return '#888'
  }
}

function AgentCard({ agent, allAgents }: { agent: Agent; allAgents: Agent[] }) {
  const [expanded, setExpanded] = useState(false)

  return (
    <div className="agent-card" onClick={() => setExpanded(!expanded)}>
      <div className="agent-header">
        <span className="agent-status" style={{ background: phaseColor(agent.phase) }} />
        <h3>{agent.name}</h3>
        <span className="agent-phase">{agent.phase}</span>
      </div>

      <div className="agent-meta">
        {agent.provider && agent.model && (
          <span className="tag model">{agent.provider}/{agent.model}</span>
        )}
        {agent.channels?.map(ch => (
          <span key={ch} className="tag channel">{ch}</span>
        ))}
        {agent.a2aEnabled && <span className="tag a2a">A2A</span>}
        {agent.workspaceMode === 'persistent' && (
          <span className="tag pvc">{agent.workspacePVC || 'PVC'}</span>
        )}
      </div>

      {agent.soul && (
        <p className="agent-soul">{agent.soul}</p>
      )}

      {agent.a2aPeers && agent.a2aPeers.length > 0 && (
        <div className="agent-peers">
          <span className="label">Peers:</span>
          {agent.a2aPeers.map(p => {
            const peerAgent = allAgents.find(a => a.name === p)
            const peerPhase = peerAgent?.phase || 'Unknown'
            return (
              <span key={p} className="peer" style={{ borderColor: phaseColor(peerPhase) }}>
                {p}
              </span>
            )
          })}
        </div>
      )}

      {expanded && (
        <div className="agent-detail">
          {agent.budgetDaily ? (
            <div className="detail-row">
              <span className="label">Budget:</span>
              ${agent.budgetDaily}/day, ${agent.budgetMonthly}/mo
            </div>
          ) : null}
          {agent.toolDeny && agent.toolDeny.length > 0 && (
            <div className="detail-row">
              <span className="label">Denied tools:</span>
              {agent.toolDeny.join(', ')}
            </div>
          )}
          {agent.a2aSkills && agent.a2aSkills.length > 0 && (
            <div className="detail-row">
              <span className="label">A2A Skills:</span>
              {agent.a2aSkills.join(', ')}
            </div>
          )}
          {agent.workspaceMode && (
            <div className="detail-row">
              <span className="label">Workspace:</span>
              {agent.workspaceMode} {agent.reclaimPolicy && `(${agent.reclaimPolicy})`}
            </div>
          )}
        </div>
      )}
    </div>
  )
}

function A2AMesh({ agents }: { agents: Agent[] }) {
  const a2aAgents = agents.filter(a => a.a2aEnabled)
  if (a2aAgents.length === 0) return null

  const edges: { from: string; to: string }[] = []
  for (const a of a2aAgents) {
    for (const peer of a.a2aPeers || []) {
      if (!edges.some(e => e.from === peer && e.to === a.name)) {
        edges.push({ from: a.name, to: peer })
      }
    }
  }

  const r = 120
  const cx = 160
  const cy = 150
  const positions: Record<string, { x: number; y: number }> = {}
  a2aAgents.forEach((a, i) => {
    const angle = (2 * Math.PI * i) / a2aAgents.length - Math.PI / 2
    positions[a.name] = {
      x: cx + r * Math.cos(angle),
      y: cy + r * Math.sin(angle),
    }
  })

  return (
    <div className="a2a-mesh">
      <h3>A2A Mesh</h3>
      <svg width="320" height="300" viewBox="0 0 320 300">
        {edges.map((e, i) => {
          const from = positions[e.from]
          const to = positions[e.to]
          if (!from || !to) return null
          return (
            <line
              key={i}
              x1={from.x} y1={from.y}
              x2={to.x} y2={to.y}
              stroke="#4ecca3"
              strokeWidth={2}
              opacity={0.5}
            />
          )
        })}
        {a2aAgents.map(a => {
          const pos = positions[a.name]
          return (
            <g key={a.name}>
              <circle
                cx={pos.x} cy={pos.y} r={24}
                fill="#1a1a2e"
                stroke={phaseColor(a.phase)}
                strokeWidth={2}
              />
              <text
                x={pos.x} y={pos.y + 4}
                textAnchor="middle"
                fill="#e0e0e0"
                fontSize={10}
              >
                {a.name}
              </text>
            </g>
          )
        })}
      </svg>
    </div>
  )
}

function App() {
  const [agents, setAgents] = useState<Agent[]>([])
  const [channels, setChannels] = useState<Channel[]>([])
  const [summary, setSummary] = useState<Summary | null>(null)
  const [error, setError] = useState<string | null>(null)

  const fetchAll = () => {
    Promise.all([
      fetch(`${API}/api/agents`).then(r => r.json()),
      fetch(`${API}/api/channels`).then(r => r.json()),
      fetch(`${API}/api/summary`).then(r => r.json()),
    ])
      .then(([a, c, s]) => {
        setAgents(a || [])
        setChannels(c || [])
        setSummary(s)
        setError(null)
      })
      .catch(e => setError(e.message))
  }

  useEffect(() => {
    fetchAll()
    const interval = setInterval(fetchAll, 5000)
    return () => clearInterval(interval)
  }, [])

  return (
    <div className="app">
      <header>
        <h1>Clawbernetes Control Plane</h1>
        {summary && (
          <div className="summary-bar">
            <span className="stat">{summary.totalAgents} agents</span>
            <span className="stat running">{summary.runningAgents} running</span>
            <span className="stat">{summary.totalChannels} channels</span>
            <span className="stat">{summary.a2aConnections} A2A links</span>
          </div>
        )}
      </header>

      {error && <div className="error">API Error: {error}</div>}

      <div className="main-grid">
        <div className="agents-section">
          <h2>Agents</h2>
          <div className="agents-grid">
            {agents.map(a => (
              <AgentCard key={a.name} agent={a} allAgents={agents} />
            ))}
          </div>
        </div>

        <div className="sidebar">
          <A2AMesh agents={agents} />

          <div className="channels-section">
            <h3>Channels</h3>
            {channels.map(ch => (
              <div key={ch.name} className="channel-item">
                <span className={`channel-dot ${ch.enabled ? 'on' : 'off'}`} />
                <span className="channel-name">{ch.name}</span>
                <span className="channel-type">{ch.type}</span>
              </div>
            ))}
          </div>
        </div>
      </div>
    </div>
  )
}

export default App
