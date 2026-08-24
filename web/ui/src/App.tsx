import {useQuery} from "@tanstack/react-query";
import {Navigate, Route, Routes} from "react-router-dom";
import {api} from "./api";
import ProjectsPage from "./pages/ProjectsPage";
import ProjectPage from "./pages/ProjectPage";

export default function App() {
  const workspace = useQuery({queryKey: ["workspace"], queryFn: api.workspace});
  return (
    <div className="app">
      <header className="titlebar">
        <b>Loomwork</b>
        <span className="dim">QA workbench</span>
        <span className="spacer" />
        <span className="mono dim">{workspace.data?.home || "workspace loading…"}</span>
      </header>
      <main className="app-body">
        <Routes>
          <Route path="/" element={<ProjectsPage />} />
          <Route path="/projects/:projectRef" element={<ProjectPage />} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </main>
    </div>
  );
}
