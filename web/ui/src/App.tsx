import {useQuery} from "@tanstack/react-query";
import {Link, Navigate, Route, Routes, useParams} from "react-router-dom";
import {api} from "./api";
import {Icon} from "./components/Icons";
import {useTheme, type ThemePreference} from "./lib/theme";
import ProjectsPage from "./pages/ProjectsPage";
import ProjectPage from "./pages/ProjectPage";

const themeOrder: ThemePreference[] = ["system", "light", "dark"];

// Keying the desktop by project remounts it, so per-project tab and chat state
// never leaks from one project into another.
function ProjectRoute() {
  const {projectRef = ""} = useParams();
  return <ProjectPage key={projectRef} />;
}

export default function App() {
  const workspace = useQuery({queryKey: ["workspace"], queryFn: api.workspace});
  const theme = useTheme();
  const ThemeIcon = theme.preference === "system" ? Icon.Monitor : theme.resolved === "dark" ? Icon.Moon : Icon.Sun;
  const nextTheme = themeOrder[(themeOrder.indexOf(theme.preference) + 1) % themeOrder.length];

  return (
    <div className="app">
      <header className="titlebar">
        <Link to="/" className="brand">
          <span className="brand-mark" aria-hidden="true"><Icon.Layers size={16} /></span>
          <span>Loomwork</span>
        </Link>
        <span className="titlebar-sub">QA workbench</span>
        <span className="spacer" />
        {workspace.data?.home && (
          <span className="workspace-path" title={`Workspace: ${workspace.data.home}`}>
            <span className="muted">workspace</span> <code>{workspace.data.home}</code>
          </span>
        )}
        <a className="icon-btn" href="/api/openapi.json" target="_blank" rel="noreferrer" title="OpenAPI document" aria-label="OpenAPI document">
          <Icon.Book size={16} />
        </a>
        <button
          type="button"
          className="icon-btn"
          onClick={() => theme.setPreference(nextTheme)}
          title={`Theme: ${theme.preference} (click for ${nextTheme})`}
          aria-label={`Theme: ${theme.preference}`}
        >
          <ThemeIcon size={16} />
        </button>
      </header>
      <main className="app-body">
        <Routes>
          <Route path="/" element={<ProjectsPage />} />
          <Route path="/projects/:projectRef" element={<ProjectRoute />} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </main>
    </div>
  );
}
