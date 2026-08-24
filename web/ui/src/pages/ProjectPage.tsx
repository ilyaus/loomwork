import {FormEvent, PointerEvent, useCallback, useEffect, useMemo, useState} from "react";
import {useMutation, useQuery, useQueryClient} from "@tanstack/react-query";
import {Link, useParams} from "react-router-dom";
import {api} from "../api";
import ChatDock from "../components/ChatDock";
import {SafeLink, Viewer} from "../components/Viewers";
import type {DocumentSource, TreeItem} from "../types";

const splitterKey = "loomwork.projectTreeWidth";
const minimumTreeWidth = 220;
const maximumTreeWidth = 480;

type Tab = {
  key: string;
  item?: TreeItem;
  name: string;
};

export default function ProjectPage() {
  const {projectRef = ""} = useParams();
  const queryClient = useQueryClient();
  const project = useQuery({queryKey: ["project", projectRef], queryFn: () => api.getProject(projectRef)});
  const tree = useQuery({queryKey: ["project-items", projectRef], queryFn: () => api.projectItems(projectRef)});
  const [treeWidth, setTreeWidth] = useState(() => {
    const stored = Number(localStorage.getItem(splitterKey));
    return Number.isFinite(stored) ? Math.min(maximumTreeWidth, Math.max(minimumTreeWidth, stored)) : 280;
  });
  const [tabs, setTabs] = useState<Tab[]>([{key: "overview", name: "Project overview"}]);
  const [activeKey, setActiveKey] = useState("overview");

  const refresh = useCallback(() => {
    void queryClient.invalidateQueries({queryKey: ["project", projectRef]});
    void queryClient.invalidateQueries({queryKey: ["project-items", projectRef]});
  }, [projectRef, queryClient]);

  function openItem(item: TreeItem) {
    const key = `${item.family}:${item.ref}:${item.version || "current"}`;
    setTabs((current) => current.some((tab) => tab.key === key)
      ? current
      : [...current, {key, item, name: item.name}],
    );
    setActiveKey(key);
  }

  function closeTab(key: string) {
    if (key === "overview") return;
    setTabs((current) => {
      const index = current.findIndex((tab) => tab.key === key);
      const next = current.filter((tab) => tab.key !== key);
      if (activeKey === key) setActiveKey(next[Math.max(0, index - 1)]?.key || "overview");
      return next;
    });
  }

  function resize(event: PointerEvent<HTMLDivElement>) {
    event.currentTarget.setPointerCapture(event.pointerId);
    const startX = event.clientX;
    const startWidth = treeWidth;
    const target = event.currentTarget;
    const move = (moveEvent: globalThis.PointerEvent) => {
      setTreeWidth(Math.min(maximumTreeWidth, Math.max(minimumTreeWidth, startWidth + moveEvent.clientX - startX)));
    };
    const stop = () => {
      target.removeEventListener("pointermove", move);
      target.removeEventListener("pointerup", stop);
      target.removeEventListener("pointercancel", stop);
    };
    target.addEventListener("pointermove", move);
    target.addEventListener("pointerup", stop);
    target.addEventListener("pointercancel", stop);
  }

  useEffect(() => {
    localStorage.setItem(splitterKey, String(treeWidth));
  }, [treeWidth]);

  const activeTab = tabs.find((tab) => tab.key === activeKey) || tabs[0];
  const activeArtifactRef = activeTab.item?.family === "artifacts" ? activeTab.item.ref : undefined;

  if (project.isLoading || tree.isLoading) return <div className="empty-state">Loading project desktop…</div>;
  if (project.error) return <div className="error-panel">{project.error.message}</div>;
  if (tree.error) return <div className="error-panel">{tree.error.message}</div>;
  if (!project.data || !tree.data) return null;

  return (
    <div className="project-desktop">
      <div className="project-toolbar">
        <Link to="/" className="back-link">Projects</Link>
        <span>/</span>
        <b>{project.data.name}</b>
        <span className="mono dim">{project.data.id}</span>
        <span className="spacer" />
        <span>{tree.data.groups.reduce((count, group) => count + group.items.length, 0)} items</span>
      </div>
      <div className="shell">
        <aside className="tree-pane pane" style={{width: treeWidth}}>
          <div className="pane-h">
            <span className="tag tg-doc">project</span>
            <b>Explorer</b>
          </div>
          <button className={`tree-overview ${activeKey === "overview" ? "selected" : ""}`} onClick={() => setActiveKey("overview")}>
            <span>⌂</span> Project overview
          </button>
          <div className="tree-scroll">
            {tree.data.groups.map((group) => (
              <TreeGroup
                family={group.family}
                label={group.label}
                items={group.items}
                activeKey={activeKey}
                onOpen={openItem}
                key={group.family}
              />
            ))}
          </div>
        </aside>
        <div className="splitter" onPointerDown={resize} role="separator" aria-orientation="vertical" />
        <section className="work-area">
          <div className="viewer-region pane">
            <div className="viewer-tabs">
              {tabs.map((tab) => (
                <button className={`viewer-tab ${tab.key === activeKey ? "on" : ""}`} onClick={() => setActiveKey(tab.key)} key={tab.key}>
                  <span>{tab.name}</span>
                  {tab.item?.version && <small>v{tab.item.version}</small>}
                  {tab.key !== "overview" && (
                    <i onClick={(event) => {event.stopPropagation(); closeTab(tab.key);}}>×</i>
                  )}
                </button>
              ))}
            </div>
            <div className="viewer-content">
              {activeTab.key === "overview"
                ? <ProjectOverview projectRef={projectRef} refresh={refresh} />
                : activeTab.item && <ActiveViewer projectRef={projectRef} item={activeTab.item} refresh={refresh} />}
            </div>
          </div>
          <ChatDock projectRef={projectRef} activeArtifactRef={activeArtifactRef} />
        </section>
      </div>
    </div>
  );
}

function TreeGroup({
  label,
  items,
  activeKey,
  onOpen,
}: {
  family: string;
  label: string;
  items: TreeItem[];
  activeKey: string;
  onOpen: (item: TreeItem) => void;
}) {
  const [open, setOpen] = useState(true);
  return (
    <section className="tree-group">
      <button className="tree-group-label" onClick={() => setOpen((value) => !value)}>
        <span>{open ? "▾" : "▸"}</span>
        <b>{label}</b>
        <small>{items.length}</small>
      </button>
      {open && (
        <div>
          {items.map((item) => (
            <div key={`${item.family}:${item.ref}`}>
              <TreeNode item={item} activeKey={activeKey} onOpen={onOpen} />
              {item.children?.map((child) => (
                <TreeNode item={child} activeKey={activeKey} onOpen={onOpen} nested key={`${child.family}:${child.ref}`} />
              ))}
            </div>
          ))}
          {items.length === 0 && <div className="tree-empty">Empty</div>}
        </div>
      )}
    </section>
  );
}

function TreeNode({item, activeKey, onOpen, nested = false}: {
  item: TreeItem;
  activeKey: string;
  onOpen: (item: TreeItem) => void;
  nested?: boolean;
}) {
  const key = `${item.family}:${item.ref}:${item.version || "current"}`;
  return (
    <button className={`tree-node ${nested ? "nested" : ""} ${activeKey === key ? "selected" : ""}`} onClick={() => onOpen(item)}>
      <span className={`node-dot ${item.artifactType}`} />
      <span>{item.name}</span>
      {item.status && <small>{item.status}</small>}
      {item.version && <i>v{item.version}</i>}
    </button>
  );
}

function ActiveViewer({projectRef, item, refresh}: {projectRef: string; item: TreeItem; refresh: () => void}) {
  const document = useQuery({
    queryKey: ["item", projectRef, item.family, item.ref, item.version],
    queryFn: () => api.projectItem(projectRef, item.family, item.ref, item.version),
  });
  if (document.isLoading) return <div className="empty-state">Opening {item.name}…</div>;
  if (document.error) return <div className="error-panel">{document.error.message}</div>;
  if (!document.data) return null;
  return (
    <Viewer
      key={`${document.data.family}:${document.data.ref}:${document.data.version || "current"}`}
      document={document.data}
      projectRef={projectRef}
      refresh={refresh}
    />
  );
}

function ProjectOverview({projectRef, refresh}: {projectRef: string; refresh: () => void}) {
  const project = useQuery({queryKey: ["project", projectRef], queryFn: () => api.getProject(projectRef)});
  const [sourceName, setSourceName] = useState("");
  const [sourceType, setSourceType] = useState<DocumentSource["type"]>("github");
  const [sourceURL, setSourceURL] = useState("");
  const [requirementText, setRequirementText] = useState("");
  const [requirementTags, setRequirementTags] = useState("");
  const addSource = useMutation({
    mutationFn: () => api.addSource(projectRef, {name: sourceName, type: sourceType, url: sourceURL}),
    onSuccess: () => {
      setSourceName("");
      setSourceURL("");
      refresh();
    },
  });
  const addRequirement = useMutation({
    mutationFn: () => api.createRequirement(projectRef, {
      text: requirementText,
      tags: requirementTags.split(",").map((tag) => tag.trim()).filter(Boolean),
    }),
    onSuccess: () => {
      setRequirementText("");
      setRequirementTags("");
      refresh();
    },
  });
  const latestArtifacts = useMemo(() => project.data?.artifacts || [], [project.data]);
  function submitSource(event: FormEvent) {
    event.preventDefault();
    addSource.mutate();
  }
  function submitRequirement(event: FormEvent) {
    event.preventDefault();
    addRequirement.mutate();
  }
  if (!project.data) return <div className="empty-state">Loading overview…</div>;
  return (
    <div className="overview">
      <header className="overview-title">
        <div>
          <span className="tag tg-spec">project</span>
          <h1>{project.data.name}</h1>
          <p>{project.data.description || "No project description"}</p>
        </div>
        <div className="overview-counts">
          <span><b>{project.data.sources?.length || 0}</b> sources</span>
          <span><b>{latestArtifacts.length}</b> artifact revisions</span>
        </div>
      </header>
      <div className="overview-grid">
        <section className="card">
          <div className="pane-h"><span className="tag tg-doc">sources</span><b>Documentation links</b></div>
          <div className="card-body source-list">
            {project.data.sources?.map((source) => (
              <div key={source.name}>
                <b>{source.name}</b>
                <span>{source.type}</span>
                <SafeLink value={source.url || source.localPath || source.s3Uri || ""} />
              </div>
            ))}
            {!project.data.sources?.length && <p className="dim">No sources linked.</p>}
          </div>
          <form className="inline-form" onSubmit={submitSource}>
            <input value={sourceName} onChange={(event) => setSourceName(event.target.value)} placeholder="Source name" required />
            <select value={sourceType} onChange={(event) => setSourceType(event.target.value as DocumentSource["type"])}>
              <option value="github">GitHub</option>
              <option value="ado">Azure DevOps</option>
              <option value="confluence">Confluence</option>
              <option value="other">Other</option>
            </select>
            <input value={sourceURL} onChange={(event) => setSourceURL(event.target.value)} placeholder="https://…" required />
            <button className="btn">Add source</button>
          </form>
        </section>
        <section className="card">
          <div className="pane-h"><span className="tag tg-test">requirement</span><b>Capture requirement</b></div>
          <form className="stack-form" onSubmit={submitRequirement}>
            <textarea value={requirementText} onChange={(event) => setRequirementText(event.target.value)} rows={7} placeholder="Requirement text" required />
            <input value={requirementTags} onChange={(event) => setRequirementTags(event.target.value)} placeholder="tags, comma-separated" />
            <button className="btn primary" disabled={addRequirement.isPending}>Create requirement</button>
          </form>
        </section>
      </div>
    </div>
  );
}
