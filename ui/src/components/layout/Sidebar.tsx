import { NavLink } from 'react-router-dom'

interface NavItem {
  to: string
  label: string
}

const fleetNav: NavItem[] = [{ to: '/', label: 'Dashboard' }]

const resourceNav: NavItem[] = [
  { to: '/agents', label: 'Agents' },
  { to: '/channels', label: 'Channels' },
  { to: '/policies', label: 'Policies' },
  { to: '/skillsets', label: 'Skill Sets' },
  { to: '/gateways', label: 'Gateways' },
  { to: '/observabilities', label: 'Observability' },
]

function linkClass({ isActive }: { isActive: boolean }) {
  const base = 'block rounded-md px-3 py-2 text-sm transition-colors'
  return isActive
    ? `${base} bg-claw-accent/15 text-claw-accent font-medium`
    : `${base} text-claw-text hover:bg-claw-border/40 hover:text-claw-accent`
}

export default function Sidebar() {
  return (
    <aside className="fixed left-0 top-0 flex h-screen w-56 flex-col border-r border-claw-border bg-claw-card">
      <div className="flex h-14 items-center px-4">
        <span className="text-lg font-bold text-claw-accent">Clawbernetes</span>
      </div>

      <nav className="flex-1 overflow-y-auto px-3 py-4">
        <div className="mb-4">
          <p className="mb-1 px-3 text-xs font-semibold uppercase tracking-wider text-claw-dim">
            Fleet
          </p>
          {fleetNav.map((item) => (
            <NavLink key={item.to} to={item.to} end className={linkClass}>
              {item.label}
            </NavLink>
          ))}
        </div>

        <div>
          <p className="mb-1 px-3 text-xs font-semibold uppercase tracking-wider text-claw-dim">
            Resources
          </p>
          {resourceNav.map((item) => (
            <NavLink key={item.to} to={item.to} className={linkClass}>
              {item.label}
            </NavLink>
          ))}
        </div>
      </nav>
    </aside>
  )
}
