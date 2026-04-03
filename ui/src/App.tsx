import { Routes, Route } from 'react-router-dom'
import Layout from './components/layout/Layout'
import DashboardPage from './pages/DashboardPage'
import AgentListPage from './pages/AgentListPage'
import AgentCreatePage from './pages/AgentCreatePage'
import AgentDetailPage from './pages/AgentDetailPage'
import AgentEditPage from './pages/AgentEditPage'
import ChannelListPage from './pages/ChannelListPage'
import ChannelCreatePage from './pages/ChannelCreatePage'
import ChannelEditPage from './pages/ChannelEditPage'
import PolicyListPage from './pages/PolicyListPage'
import PolicyCreatePage from './pages/PolicyCreatePage'
import PolicyEditPage from './pages/PolicyEditPage'
import SkillSetListPage from './pages/SkillSetListPage'
import SkillSetCreatePage from './pages/SkillSetCreatePage'
import SkillSetEditPage from './pages/SkillSetEditPage'
import GatewayListPage from './pages/GatewayListPage'
import GatewayCreatePage from './pages/GatewayCreatePage'
import GatewayEditPage from './pages/GatewayEditPage'
import ObservabilityListPage from './pages/ObservabilityListPage'
import ObservabilityCreatePage from './pages/ObservabilityCreatePage'
import ObservabilityEditPage from './pages/ObservabilityEditPage'

function App() {
  return (
    <Routes>
      <Route element={<Layout />}>
        <Route index element={<DashboardPage />} />

        <Route path="agents" element={<AgentListPage />} />
        <Route path="agents/new" element={<AgentCreatePage />} />
        <Route path="agents/:name" element={<AgentDetailPage />} />
        <Route path="agents/:name/edit" element={<AgentEditPage />} />

        <Route path="channels" element={<ChannelListPage />} />
        <Route path="channels/new" element={<ChannelCreatePage />} />
        <Route path="channels/:name/edit" element={<ChannelEditPage />} />

        <Route path="policies" element={<PolicyListPage />} />
        <Route path="policies/new" element={<PolicyCreatePage />} />
        <Route path="policies/:name/edit" element={<PolicyEditPage />} />

        <Route path="skillsets" element={<SkillSetListPage />} />
        <Route path="skillsets/new" element={<SkillSetCreatePage />} />
        <Route path="skillsets/:name/edit" element={<SkillSetEditPage />} />

        <Route path="gateways" element={<GatewayListPage />} />
        <Route path="gateways/new" element={<GatewayCreatePage />} />
        <Route path="gateways/:name/edit" element={<GatewayEditPage />} />

        <Route path="observabilities" element={<ObservabilityListPage />} />
        <Route path="observabilities/new" element={<ObservabilityCreatePage />} />
        <Route path="observabilities/:name/edit" element={<ObservabilityEditPage />} />
      </Route>
    </Routes>
  )
}

export default App
