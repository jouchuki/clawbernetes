import { Routes, Route } from 'react-router-dom'
import Layout from './components/layout/Layout'
import DashboardPage from './pages/DashboardPage'
import AgentListPage from './pages/AgentListPage'
import ChannelListPage from './pages/ChannelListPage'
import PolicyListPage from './pages/PolicyListPage'
import SkillSetListPage from './pages/SkillSetListPage'
import GatewayListPage from './pages/GatewayListPage'
import ObservabilityListPage from './pages/ObservabilityListPage'
import AgentDetailPage from './pages/AgentDetailPage'

function App() {
  return (
    <Routes>
      <Route element={<Layout />}>
        <Route index element={<DashboardPage />} />
        <Route path="agents" element={<AgentListPage />} />
        <Route path="agents/:name" element={<AgentDetailPage />} />
        <Route path="channels" element={<ChannelListPage />} />
        <Route path="policies" element={<PolicyListPage />} />
        <Route path="skillsets" element={<SkillSetListPage />} />
        <Route path="gateways" element={<GatewayListPage />} />
        <Route path="observabilities" element={<ObservabilityListPage />} />
      </Route>
    </Routes>
  )
}

export default App
