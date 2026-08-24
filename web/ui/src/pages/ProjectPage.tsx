import {FormEvent, PointerEvent, useCallback, useEffect, useMemo, useState} from "react";
import {useMutation, useQuery, useQueryClient} from "@tanstack/react-query";
import {Link, useParams} from "react-router-dom";
import {api} from "../api";
import ChatDock from "../components/ChatDock";
import {SafeLink, Viewer} from "../components/Viewers";
import type {DocumentSource, Requirement, TreeItem} from "../types";

const splitterKey = "loomwork.projectTreeWidth";
const minimumTreeWidth = 220;
const maximumTreeWidth = 480;

type Tab = {
  key: string;
  item?: TreeItem;
  family?: string;
  name: string;
};

const tabKey = (item: TreeItem) => `${item.family}:${item.ref}`;

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
    void queryClient.invalidateQueries({queryKey: ["requirements", projectRef]});
  }, [projectRef, queryClient]);

  function openItem(item: TreeItem) {
    const key = tabKey(item);
    setTabs((current) => current.some((tab) => tab.key === key)
      ? current
      : [...current, {key, item, name: item.name}],
    );
    setActiveKey(key);
  }

  function openFamily(family: string, label: string) {
    const key = `family:${family}`;
    setTabs((current) => current.some((tab) => tab.key === key)
      ? current
      : [...current, {key, family, name: label}],
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
  useEffect(() => {
    if (!tree.data) return;
    const currentItems = new Map<string, TreeItem>();
    for (const group of tree.data.groups) {
      for (const item of group.items) {
        currentItems.set(tabKey(item), item);
        for (const child of item.children || []) currentItems.set(tabKey(child), child);
      }
    }
    setTabs((current) => current.map((tab) => {
      if (!tab.item) return tab;
      const latest = currentItems.get(tab.key);
      return latest ? {...tab, item: latest, name: latest.name} : tab;
    }));
  }, [tree.data]);

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
                onOpenFamily={openFamily}
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
                : activeTab.family
                  ? (
                    <FamilyList
                      family={activeTab.family}
                      projectRef={projectRef}
                      items={tree.data.groups.find((group) => group.family === activeTab.family)?.items || []}
                      refresh={refresh}
                    />
                  )
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
  family,
  label,
  items,
  activeKey,
  onOpen,
  onOpenFamily,
}: {
  family: string;
  label: string;
  items: TreeItem[];
  activeKey: string;
  onOpen: (item: TreeItem) => void;
  onOpenFamily: (family: string, label: string) => void;
}) {
  const [open, setOpen] = useState(true);
  const listFamily = family === "requirements" || family === "test-suites";
  if (listFamily) {
    return (
      <section className="tree-group">
        <button
          className={`tree-group-label family-link ${activeKey === `family:${family}` ? "selected" : ""}`}
          onClick={() => onOpenFamily(family, label)}
        >
          <span>▤</span>
          <b>{label}</b>
          <small>{items.length}</small>
        </button>
      </section>
    );
  }
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
  const key = tabKey(item);
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
    queryKey: ["item", projectRef, item.family, item.ref],
    queryFn: () => api.projectItem(projectRef, item.family, item.ref),
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

function FamilyList({
  family,
  projectRef,
  items,
  refresh,
}: {
  family: string;
  projectRef: string;
  items: TreeItem[];
  refresh: () => void;
}) {
  if (family === "requirements") {
    return <RequirementList projectRef={projectRef} items={items} refresh={refresh} />;
  }
  return <TestSuiteList projectRef={projectRef} items={items} refresh={refresh} />;
}

function InlineDetail({projectRef, item, refresh}: {projectRef: string; item: TreeItem; refresh: () => void}) {
  return (
    <div className="inline-detail">
      <ActiveViewer projectRef={projectRef} item={item} refresh={refresh} />
    </div>
  );
}

type RequirementEditMode = "amend" | "new-version";

function RequirementList({
  projectRef,
  items,
  refresh,
}: {
  projectRef: string;
  items: TreeItem[];
  refresh: () => void;
}) {
  const requirements = useQuery({
    queryKey: ["requirements", projectRef],
    queryFn: () => api.listRequirements(projectRef),
  });
  const treeItems = useMemo(() => new Map(items.map((item) => [item.ref, item])), [items]);
  const queryClient = useQueryClient();
  const [editing, setEditing] = useState<{id: string; mode: RequirementEditMode} | null>(null);
  const [text, setText] = useState("");
  const [tags, setTags] = useState("");
  const save = useMutation({
    mutationFn: () => {
      if (!editing) throw new Error("no requirement is being edited");
      const body = {
        text,
        tags: tags.split(",").map((tag) => tag.trim()).filter(Boolean),
      };
      return editing.mode === "amend"
        ? api.amendRequirement(projectRef, editing.id, body)
        : api.updateRequirement(projectRef, editing.id, body);
    },
    onSuccess: async (updated) => {
      const item = treeItems.get(updated.id);
      if (item) {
        queryClient.setQueryData(
          ["item", projectRef, item.family, item.ref],
          (current: unknown) => current && typeof current === "object"
            ? {...current, version: updated.version, body: updated}
            : current,
        );
      }
      setEditing(null);
      await queryClient.invalidateQueries({queryKey: ["requirements", projectRef]});
      await queryClient.invalidateQueries({queryKey: ["item", projectRef]});
      await queryClient.invalidateQueries({queryKey: ["requirement-history", projectRef, updated.id]});
      refresh();
    },
  });
  const status = useMutation({
    mutationFn: ({id, next}: {id: string; next: "active" | "obsolete"}) =>
      api.setRequirementStatus(projectRef, id, next),
    onSuccess: async (updated) => {
      const item = treeItems.get(updated.id);
      if (item) {
        queryClient.setQueryData(
          ["item", projectRef, item.family, item.ref],
          (current: unknown) => current && typeof current === "object"
            ? {...current, version: updated.version, body: updated}
            : current,
        );
      }
      await queryClient.invalidateQueries({queryKey: ["requirements", projectRef]});
      await queryClient.invalidateQueries({queryKey: ["item", projectRef]});
      await queryClient.invalidateQueries({queryKey: ["requirement-history", projectRef, updated.id]});
      refresh();
    },
  });
  function startEditing(requirement: Requirement, mode: RequirementEditMode) {
    setEditing({id: requirement.id, mode});
    setText(requirement.text);
    setTags((requirement.tags || []).join(", "));
  }
  function cancelEditing() {
    setEditing(null);
    setText("");
    setTags("");
  }
  function submit(event: FormEvent) {
    event.preventDefault();
    save.mutate();
  }
  if (requirements.isLoading) return <div className="empty-state">Loading requirements…</div>;
  if (requirements.error) return <div className="error-panel">{requirements.error.message}</div>;
  return (
    <div className="entity-list">
      <header className="entity-list-header">
        <div>
          <span className="tag tg-test">requirements</span>
          <h1>Requirements</h1>
          <p>Scan the complete current set; edit and manage each requirement in place.</p>
        </div>
        <b>{requirements.data?.length || 0}</b>
      </header>
      <div className="entity-rows">
        {requirements.data?.map((requirement: Requirement) => {
          const activeEdit = editing?.id === requirement.id;
          return (
            <div className="entity-item" key={requirement.id}>
              <div className="entity-row requirement-row">
                <span className={`tag ${requirement.status === "active" ? "tg-doc" : "tg-log"}`}>
                  {requirement.status}
                </span>
                <span className="entity-primary">
                  <b>{requirement.id}</b>
                  <span>{requirement.text}</span>
                  <small>{requirement.tags?.join(" · ") || "No tags"}</small>
                </span>
                <span className="mono dim">v{requirement.version}</span>
                <span className="requirement-actions">
                  <button className="btn" onClick={() => startEditing(requirement, "amend")} disabled={activeEdit || save.isPending}>
                    Edit
                  </button>
                  <button className="btn" onClick={() => startEditing(requirement, "new-version")} disabled={activeEdit || save.isPending}>
                    New version
                  </button>
                  <button
                    className="btn"
                    onClick={() => status.mutate({id: requirement.id, next: requirement.status === "active" ? "obsolete" : "active"})}
                    disabled={status.isPending || activeEdit}
                  >
                    Mark {requirement.status === "active" ? "obsolete" : "active"}
                  </button>
                </span>
              </div>
              {activeEdit && (
                <form className="requirement-inline-edit" onSubmit={submit}>
                  <textarea value={text} onChange={(event) => setText(event.target.value)} rows={3} required />
                  <input value={tags} onChange={(event) => setTags(event.target.value)} placeholder="tags, comma-separated" />
                  <div className="requirement-edit-actions">
                    <button className="btn primary" disabled={save.isPending}>
                      {editing?.mode === "amend" ? "Save" : "Save new version"}
                    </button>
                    <button className="btn" type="button" onClick={cancelEditing} disabled={save.isPending}>Cancel</button>
                  </div>
                  {save.error && <div className="error-panel">{save.error.message}</div>}
                </form>
              )}
            </div>
          );
        })}
        {!requirements.data?.length && <div className="empty-state">No requirements yet.</div>}
      </div>
    </div>
  );
}

function TestSuiteList({projectRef, items, refresh}: {projectRef: string; items: TreeItem[]; refresh: () => void}) {
  const testCount = items.reduce((count, item) => count + (item.children?.length || 0), 0);
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  function toggle(key: string) {
    setExpanded((current) => {
      const next = new Set(current);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  }
  return (
    <div className="entity-list">
      <header className="entity-list-header">
        <div>
          <span className="tag tg-test">test suites</span>
          <h1>Test suites</h1>
          <p>Review every suite and case from one surface; expand any row to inspect it in place.</p>
        </div>
        <b>{items.length} <small>suites</small><br />{testCount} <small>cases</small></b>
      </header>
      <div className="suite-list">
        {items.map((suite) => {
          const suiteOpen = expanded.has(tabKey(suite));
          return (
            <section className="suite-list-card" key={suite.ref}>
              <button className="suite-list-heading" onClick={() => toggle(tabKey(suite))} aria-expanded={suiteOpen}>
                <span className={`tag ${suite.status === "incomplete" ? "tg-log" : "tg-doc"}`}>
                  {suite.status || "ready"}
                </span>
                <span className="entity-primary">
                  <b>{suite.name}</b>
                  <small>{suite.ref}</small>
                </span>
                <span className="mono dim">v{suite.version}</span>
                <span>{suite.children?.length || 0} cases</span>
                <span className="row-arrow">{suiteOpen ? "▾" : "▸"}</span>
              </button>
              {suiteOpen && <InlineDetail projectRef={projectRef} item={suite} refresh={refresh} />}
              <div className="suite-list-cases">
                {suite.children?.map((testCase) => {
                  const caseOpen = expanded.has(tabKey(testCase));
                  return (
                    <div className={`entity-item ${caseOpen ? "open" : ""}`} key={testCase.ref}>
                      <button onClick={() => toggle(tabKey(testCase))} aria-expanded={caseOpen}>
                        <span className="node-dot test-case" />
                        <b>{testCase.name}</b>
                        <span className="mono dim">v{testCase.version}</span>
                        <span className="row-arrow">{caseOpen ? "▾" : "▸"}</span>
                      </button>
                      {caseOpen && <InlineDetail projectRef={projectRef} item={testCase} refresh={refresh} />}
                    </div>
                  );
                })}
                {!suite.children?.length && <div className="tree-empty">No test cases</div>}
              </div>
            </section>
          );
        })}
        {!items.length && <div className="empty-state">No test suites yet.</div>}
      </div>
    </div>
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
