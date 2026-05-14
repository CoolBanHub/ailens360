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
import { useT } from './i18n';
import type { LocaleKey } from './i18n/locales';

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

const BRAND = 'AILens360';

function moduleTitleKey(path: string): LocaleKey | null {
  if (path === '/login') return 'login.submit';
  if (path === '/projects' || path === '/projects/') return 'nav.allProjects';
  const m = path.match(/^\/projects\/[^/]+\/([^/]+)/);
  if (!m) return null;
  switch (m[1]) {
    case 'overview': return 'nav.module.overview';
    case 'traces':   return 'nav.module.traces';
    case 'sessions': return 'nav.module.sessions';
    case 'users':    return 'nav.module.users';
    case 'setup':    return 'nav.module.setup';
    case 'settings': return 'nav.module.settings';
    default: return null;
  }
}

function DocumentTitle() {
  const loc = useLocation();
  const t = useT();
  useEffect(() => {
    const key = moduleTitleKey(loc.pathname);
    document.title = key ? `${t(key)} | ${BRAND}` : BRAND;
  }, [loc.pathname, t]);
  return null;
}

export default function App() {
  return (
    <>
    <DocumentTitle />
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
    </>
  );
}
