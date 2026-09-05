import {PointerEvent, useCallback, useEffect, useMemo, useRef, useState} from "react";
import {useQuery, useQueryClient} from "@tanstack/react-query";
import {Link, useParams} from "react-router-dom";
import {api} from "../api";
import ChatDock from "../components/ChatDock";
import Explorer from "../components/Explorer";
import Overview from "../components/Overview";
import RequirementsView from "../components/RequirementsView";
import TestSuitesView from "../components/TestSuitesView";
import ProjectDialogs from "../components/dialogs/ProjectDialogs";
import {Icon} from "../components/Icons";
import {ErrorPanel, Spinner} from "../components/ui";
import {ItemViewer} from "../components/viewers";
import {
  DesktopContext,
  type DesktopActions,
  type DialogState,
  type Tab,
  familyKey,
  familyLabels,
  itemKey,
  itemTab,
  overviewTab,
} from "../lib/desktop";
import type {TreeItem} from "../types";

const treeWidthKey = "loomwork.projectTreeWidth";
const chatWidthKey = "loomwork.chatWidth";
const chatOpenKey = "loomwork.chatOpen";
const tabsKey = (project: string) => `loomwork.tabs.${project}`;

const clamp = (value: number, min: number, max: number) => Math.min(max, Math.max(min, value));
const readWidth = (key: string, fallback: number, min: number, max: number) => {
  const stored = Number(localStorage.getItem(key));
  return stored > 0 ? clamp(stored, min, max) : fallback;
};

function readTabs(project: string): {tabs: Tab[]; active: string} {
  try {
    const stored = JSON.parse(sessionStorage.getItem(tabsKey(project)) || "null") as {tabs: Tab[]; active: string} | null;
    if (stored?.tabs?.length && stored.tabs[0].key === "overview") return stored;
  } catch {
    // Ignore corrupt session state and start fresh.
  }
  return {tabs: [overviewTab], active: "overview"};
}

export default function ProjectPage() {
  const {projectRef = ""} = useParams();
  const queryClient = useQueryClient();
  const project = useQuery({queryKey: ["project", projectRef], queryFn: () => api.getProject(projectRef)});
  const tree = useQuery({queryKey: ["project-items", projectRef], queryFn: () => api.projectItems(projectRef)});

  const [treeWidth, setTreeWidth] = useState(() => readWidth(treeWidthKey, 272, 200, 480));
  const [chatWidth, setChatWidth] = useState(() => readWidth(chatWidthKey, 360, 280, 640));
  const [chatOpen, setChatOpen] = useState(() => localStorage.getItem(chatOpenKey) !== "false");
  const [{tabs, active}, setTabState] = useState(() => readTabs(projectRef));
  const [dialog, setDialog] = useState<DialogState>(null);

  useEffect(() => { localStorage.setItem(treeWidthKey, String(treeWidth)); }, [treeWidth]);
  useEffect(() => { localStorage.setItem(chatWidthKey, String(chatWidth)); }, [chatWidth]);
  useEffect(() => { localStorage.setItem(chatOpenKey, String(chatOpen)); }, [chatOpen]);
  useEffect(() => { sessionStorage.setItem(tabsKey(projectRef), JSON.stringify({tabs, active})); }, [projectRef, tabs, active]);

  const refresh = useCallback(() => {
    void queryClient.invalidateQueries({queryKey: ["project", projectRef]});
    void queryClient.invalidateQueries({queryKey: ["project-items", projectRef]});
    void queryClient.invalidateQueries({queryKey: ["requirements", projectRef]});
    void queryClient.invalidateQueries({queryKey: ["testability", projectRef]});
    void queryClient.invalidateQueries({queryKey: ["item", projectRef]});
    void queryClient.invalidateQueries({queryKey: ["item-history", projectRef]});
    void queryClient.invalidateQueries({queryKey: ["projects"]});
  }, [projectRef, queryClient]);

  const focusTab = useCallback((tab: Tab) => {
    setTabState((current) => ({
      tabs: current.tabs.some((existing) => existing.key === tab.key) ? current.tabs : [...current.tabs, tab],
      active: tab.key,
    }));
  }, []);

  const actions = useMemo<DesktopActions>(() => ({
    projectRef,
    openItem: (item) => focusTab(itemTab(item)),
    openFamily: (family) => focusTab({key: familyKey(family), kind: "family", family, name: familyLabels[family] || family}),
    openOverview: () => setTabState((current) => ({...current, active: "overview"})),
    openDialog: (kind, payload) => setDialog({kind, payload}),
    refresh,
  }), [projectRef, focusTab, refresh]);

  function closeTab(key: string) {
    if (key === "overview") return;
    setTabState((current) => {
      const index = current.tabs.findIndex((tab) => tab.key === key);
      const next = current.tabs.filter((tab) => tab.key !== key);
      const active = current.active === key ? (next[Math.max(0, index - 1)]?.key || "overview") : current.active;
      return {tabs: next, active};
    });
  }

  function closeOthers(key: string) {
    setTabState((current) => ({tabs: current.tabs.filter((tab) => tab.key === "overview" || tab.key === key), active: key}));
  }

  // Keep item tabs in step with the tree so a version bump or rename shows up.
  useEffect(() => {
    if (!tree.data) return;
    const latest = new Map<string, TreeItem>();
    for (const group of tree.data.groups) {
      for (const item of group.items) {
        latest.set(itemKey(item.family, item.ref), item);
        for (const child of item.children || []) latest.set(itemKey(child.family, child.ref), child);
      }
    }
    setTabState((current) => ({
      ...current,
      tabs: current.tabs.map((tab) => {
        if (tab.kind !== "item") return tab;
        const item = latest.get(tab.key);
        return item ? {...tab, name: item.name, version: item.version, artifactType: item.artifactType} : tab;
      }),
    }));
  }, [tree.data]);

  const activeTab = tabs.find((tab) => tab.key === active) || tabs[0];
  const activeTabRef = useRef<HTMLButtonElement>(null);
  useEffect(() => { activeTabRef.current?.scrollIntoView({block: "nearest", inline: "nearest"}); }, [activeTab.key]);
  const activeArtifactRef = activeTab.kind === "item" && activeTab.family === "artifacts" ? activeTab.ref : undefined;
  const activeItemLabel = activeTab.kind === "item" ? activeTab.name : undefined;

  function startResize(event: PointerEvent<HTMLDivElement>, side: "tree" | "chat") {
    event.preventDefault();
    const target = event.currentTarget;
    target.setPointerCapture(event.pointerId);
    const startX = event.clientX;
    const startWidth = side === "tree" ? treeWidth : chatWidth;
    const move = (moveEvent: globalThis.PointerEvent) => {
      const delta = moveEvent.clientX - startX;
      if (side === "tree") setTreeWidth(clamp(startWidth + delta, 200, 480));
      else setChatWidth(clamp(startWidth - delta, 280, 640));
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

  if (project.isLoading || tree.isLoading) return <div className="page-pad"><Spinner label="Opening project…" /></div>;
  if (project.error) return <div className="page-pad"><ErrorPanel error={project.error} /><Link to="/" className="btn">Back to projects</Link></div>;
  if (tree.error) return <div className="page-pad"><ErrorPanel error={tree.error} /></div>;
  if (!project.data || !tree.data) return null;

  return (
    <DesktopContext.Provider value={actions}>
      <div className="desktop">
        <div className="desktop-bar">
          <nav className="crumbs" aria-label="Breadcrumb">
            <Link to="/">Projects</Link>
            <Icon.Chevron size={13} />
            <b>{project.data.name}</b>
          </nav>
          <span className="spacer" />
          <button type="button" className={`btn ghost ${chatOpen ? "on" : ""}`} onClick={() => setChatOpen((value) => !value)} title={chatOpen ? "Hide chat" : "Show chat"}>
            <Icon.Chat size={15} /> Chat
          </button>
        </div>
        <div className="desktop-body">
          <aside className="tree-pane" style={{width: treeWidth}}>
            <Explorer groups={tree.data.groups} activeKey={activeTab.key} />
          </aside>
          <div className="splitter" onPointerDown={(event) => startResize(event, "tree")} role="separator" aria-orientation="vertical" aria-label="Resize explorer" />
          <section className="work-area">
            <div className="tab-strip" role="tablist">
              {tabs.map((tab) => (
                <button
                  type="button"
                  role="tab"
                  aria-selected={tab.key === activeTab.key}
                  className={`tab ${tab.key === activeTab.key ? "on" : ""}`}
                  onClick={() => setTabState((current) => ({...current, active: tab.key}))}
                  onAuxClick={(event) => { if (event.button === 1) closeTab(tab.key); }}
                  onDoubleClick={() => closeOthers(tab.key)}
                  title={tab.kind === "item" ? `${familyLabels[tab.family] || tab.family}: ${tab.name}` : tab.name}
                  ref={tab.key === activeTab.key ? activeTabRef : undefined}
                  key={tab.key}
                >
                  {tab.kind === "overview" && <Icon.Home size={13} />}
                  <span className="tab-name">{tab.name}</span>
                  {tab.kind === "item" && tab.version ? <small>v{tab.version}</small> : null}
                  {tab.key !== "overview" && (
                    <span
                      className="tab-close"
                      role="button"
                      tabIndex={0}
                      aria-label={`Close ${tab.name}`}
                      onClick={(event) => { event.stopPropagation(); closeTab(tab.key); }}
                      onKeyDown={(event) => {
                        if (event.key === "Enter" || event.key === " ") { event.preventDefault(); event.stopPropagation(); closeTab(tab.key); }
                      }}
                    >
                      <Icon.Close size={12} />
                    </span>
                  )}
                </button>
              ))}
            </div>
            <div className="viewer" role="tabpanel">
              {activeTab.kind === "overview" && <Overview project={project.data} groups={tree.data.groups} />}
              {activeTab.kind === "family" && activeTab.family === "requirements" && <RequirementsView />}
              {activeTab.kind === "family" && activeTab.family === "test-suites" && (
                <TestSuitesView items={tree.data.groups.find((group) => group.family === "test-suites")?.items || []} />
              )}
              {activeTab.kind === "item" && <ItemViewer key={activeTab.key} family={activeTab.family} ref_={activeTab.ref} />}
            </div>
          </section>
          {chatOpen && (
            <>
              <div className="splitter" onPointerDown={(event) => startResize(event, "chat")} role="separator" aria-orientation="vertical" aria-label="Resize chat" />
              <aside className="chat-pane" style={{width: chatWidth}}>
                <ChatDock projectRef={projectRef} activeArtifactRef={activeArtifactRef} activeLabel={activeItemLabel} onClose={() => setChatOpen(false)} />
              </aside>
            </>
          )}
        </div>
        <ProjectDialogs state={dialog} onClose={() => setDialog(null)} project={project.data} />
      </div>
    </DesktopContext.Provider>
  );
}
