import {FormEvent, useMemo, useState} from "react";
import {useMutation, useQuery, useQueryClient} from "@tanstack/react-query";
import {Link, useNavigate} from "react-router-dom";
import {api} from "../api";
import {Icon} from "../components/Icons";
import {Badge, Dialog, EmptyState, ErrorPanel, Field, Spinner} from "../components/ui";
import {percent, splitTags, timeAgo} from "../lib/format";
import type {ProjectSummary} from "../types";

export default function ProjectsPage() {
  const projects = useQuery({queryKey: ["projects"], queryFn: api.listProjects});
  const [creating, setCreating] = useState(false);
  const [filter, setFilter] = useState("");

  const visible = useMemo(() => {
    const needle = filter.trim().toLowerCase();
    const list = projects.data || [];
    if (!needle) return list;
    return list.filter((project) =>
      [project.name, project.description || "", ...(project.tags || [])].join(" ").toLowerCase().includes(needle));
  }, [projects.data, filter]);

  return (
    <div className="landing">
      <div className="landing-inner">
        <header className="landing-head">
          <div>
            <h1>Projects</h1>
            <p className="muted">
              Each project is a directory of versioned requirements, agent definitions, test suites, and reports.
            </p>
          </div>
          <div className="landing-actions">
            {(projects.data?.length || 0) > 3 && (
              <label className="search">
                <Icon.Search size={15} />
                <input value={filter} onChange={(event) => setFilter(event.target.value)} placeholder="Filter projects" />
              </label>
            )}
            <button type="button" className="btn primary" onClick={() => setCreating(true)}>
              <Icon.Plus size={15} /> New project
            </button>
          </div>
        </header>

        {projects.isLoading && <Spinner label="Loading projects…" />}
        {projects.error && <ErrorPanel error={projects.error} />}

        {projects.data && projects.data.length === 0 && (
          <EmptyState title="No projects yet" icon={<Icon.Layers size={28} />}>
            <p>Create a project to start linking documentation and capturing requirements.</p>
            <button type="button" className="btn primary" onClick={() => setCreating(true)}>
              <Icon.Plus size={15} /> Create the first project
            </button>
          </EmptyState>
        )}

        {visible.length > 0 && (
          <section className="project-grid">
            {visible.map((project) => <ProjectCard project={project} key={project.id} />)}
          </section>
        )}
        {projects.data && projects.data.length > 0 && visible.length === 0 && (
          <EmptyState title="No projects match that filter" />
        )}
      </div>
      <CreateProjectDialog open={creating} onClose={() => setCreating(false)} />
    </div>
  );
}

function ProjectCard({project}: {project: ProjectSummary}) {
  const health = project.testability;
  return (
    <Link className="project-card" to={`/projects/${encodeURIComponent(project.id)}`}>
      <div className="project-card-head">
        <h2>{project.name}</h2>
        {health.available && health.coveragePercent !== null && (
          <Badge tone={health.coveragePercent >= 80 ? "ok" : health.coveragePercent >= 40 ? "warn" : "danger"} title="Active requirements covered by at least one test case">
            {percent(health.coveragePercent)} covered
          </Badge>
        )}
      </div>
      <p className="project-card-desc">{project.description || <span className="muted">No description</span>}</p>
      <dl className="project-stats">
        <div><dt>Requirements</dt><dd>{project.activeRequirements}<small> / {project.requirements}</small></dd></div>
        <div><dt>Artifacts</dt><dd>{project.artifacts}</dd></div>
        <div><dt>Sources</dt><dd>{project.sources}</dd></div>
        <div>
          <dt>Open gaps</dt>
          <dd>{health.openGaps === null ? <span className="muted">–</span> : health.openGaps}</dd>
        </div>
      </dl>
      <footer className="project-card-foot">
        <span className="chips">
          {project.tags?.map((tag) => <span className="chip" key={tag}>{tag}</span>)}
        </span>
        <span className="muted">
          {health.lastTestedAt ? `tested ${timeAgo(health.lastTestedAt)}` : `updated ${timeAgo(project.updatedAt)}`}
        </span>
      </footer>
    </Link>
  );
}

function CreateProjectDialog({open, onClose}: {open: boolean; onClose: () => void}) {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [tags, setTags] = useState("");
  const create = useMutation({
    mutationFn: api.createProject,
    onSuccess: async (project) => {
      await queryClient.invalidateQueries({queryKey: ["projects"]});
      onClose();
      navigate(`/projects/${encodeURIComponent(project.id)}`);
    },
  });

  function submit(event: FormEvent) {
    event.preventDefault();
    create.mutate({name: name.trim(), description: description.trim(), tags: splitTags(tags)});
  }

  return (
    <Dialog open={open} title="New project" onClose={onClose} width={480}>
      <form className="form" onSubmit={submit}>
        <Field label="Name">
          <input value={name} onChange={(event) => setName(event.target.value)} placeholder="Checkout API" required autoFocus />
        </Field>
        <Field label="Description" hint="Optional. What service or area this project tests.">
          <textarea value={description} onChange={(event) => setDescription(event.target.value)} rows={3} />
        </Field>
        <Field label="Tags" hint="Comma-separated.">
          <input value={tags} onChange={(event) => setTags(event.target.value)} placeholder="checkout, payments" />
        </Field>
        {create.error && <ErrorPanel error={create.error} />}
        <div className="form-actions">
          <button type="button" className="btn" onClick={onClose}>Cancel</button>
          <button type="submit" className="btn primary" disabled={create.isPending || !name.trim()}>
            {create.isPending ? "Creating…" : "Create project"}
          </button>
        </div>
      </form>
    </Dialog>
  );
}
