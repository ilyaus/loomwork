import {FormEvent, useState} from "react";
import {useMutation, useQuery, useQueryClient} from "@tanstack/react-query";
import {Link, useNavigate} from "react-router-dom";
import {api} from "../api";

export default function ProjectsPage() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const projects = useQuery({queryKey: ["projects"], queryFn: api.listProjects});
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [tags, setTags] = useState("");
  const create = useMutation({
    mutationFn: api.createProject,
    onSuccess: async (project) => {
      await queryClient.invalidateQueries({queryKey: ["projects"]});
      navigate(`/projects/${encodeURIComponent(project.id)}`);
    },
  });

  function submit(event: FormEvent) {
    event.preventDefault();
    create.mutate({
      name,
      description,
      tags: tags.split(",").map((tag) => tag.trim()).filter(Boolean),
    });
  }

  return (
    <div className="landing">
      <section className="landing-head">
        <div>
          <span className="tag tg-doc">workspace</span>
          <h1>Projects</h1>
          <p>Open a project to inspect its requirements, agents, suites, reports, and artifacts.</p>
        </div>
        <form className="new-project" onSubmit={submit}>
          <input value={name} onChange={(event) => setName(event.target.value)} placeholder="Project name" required />
          <input value={description} onChange={(event) => setDescription(event.target.value)} placeholder="Description" />
          <input value={tags} onChange={(event) => setTags(event.target.value)} placeholder="tags, comma-separated" />
          <button className="btn primary" disabled={create.isPending}>Create project</button>
        </form>
      </section>
      {projects.isLoading && <div className="empty-state">Loading projects…</div>}
      {projects.error && <div className="error-panel">{projects.error.message}</div>}
      <section className="project-grid">
        {projects.data?.map((project) => (
          <Link className="project-card" key={project.id} to={`/projects/${encodeURIComponent(project.id)}`}>
            <div className="project-card-head">
              <h2>{project.name}</h2>
              <span className="mono dim">{project.id}</span>
            </div>
            <p>{project.description || "No description"}</p>
            <div className="project-counts">
              <span><b>{project.requirements}</b> requirements</span>
              <span><b>{project.artifacts}</b> artifacts</span>
              <span><b>{project.sources}</b> sources</span>
            </div>
            <div className="chips">
              {project.tags?.map((tag) => <span className="chip" key={tag}>{tag}</span>)}
            </div>
          </Link>
        ))}
      </section>
      {!projects.isLoading && projects.data?.length === 0 && (
        <div className="empty-state">No projects yet. Create the first one above.</div>
      )}
    </div>
  );
}
