import { Navigate, Route, Routes, useLocation } from 'react-router-dom';
import { useEffect, useState } from 'react';
import AppShell from './components/AppShell';
import Login from './pages/Login';
import ProjectsHome from './pages/ProjectsHome';
import ProjectOverview from './pages/project/Overview';
import ProjectTraces from './pages/project/Traces';
import ProjectTraceDetail from './pages/project/TraceDetail';
import ProjectSetup from './pages/project/Setup';
import ProjectSettings from './pages/project/Settings';
import { getAuth, subscribe } from './lib/auth';

function useAuthed() {
  const [authed, setAuthed] = useState(!!getAuth().token);
  useEffect(() => subscribe(() => setAuthed(!!getAuth().token)), []);
  return authed;
}

function RequireAuth({ children }: { children: React.ReactNode }) {
  const authed = useAuthed();
  const loc = useLocation();
  if (!authed) return <Navigate to="/login" replace state={{ from: loc.pathname }} />;
  return <>{children}</>;
}

export default function App() {
  return (
    <Routes>
      <Route path="/login" element={<Login />} />

      <Route
        path="/projects"
        element={
          <RequireAuth>
            <AppShell><ProjectsHome /></AppShell>
          </RequireAuth>
        }
      />

      <Route
        path="/projects/:projectId/*"
        element={
          <RequireAuth>
            <AppShell>
              <Routes>
                <Route index            element={<Navigate to="overview" replace />} />
                <Route path="overview"  element={<ProjectOverview />} />
                <Route path="traces"            element={<ProjectTraces />} />
                <Route path="traces/:traceId"   element={<ProjectTraceDetail />} />
                <Route path="setup"     element={<ProjectSetup />} />
                <Route path="settings"  element={<ProjectSettings />} />
                <Route path="*"         element={<Navigate to="overview" replace />} />
              </Routes>
            </AppShell>
          </RequireAuth>
        }
      />

      <Route path="/" element={<Navigate to="/projects" replace />} />
      <Route path="*" element={<Navigate to="/projects" replace />} />
    </Routes>
  );
}
