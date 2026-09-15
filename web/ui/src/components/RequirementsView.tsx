import {FormEvent, useEffect, useMemo, useState} from "react";
import {useMutation, useQuery, useQueryClient} from "@tanstack/react-query";
import {api} from "../api";
import {Icon} from "./Icons";
import {Badge, Chips, EmptyState, ErrorPanel, Field, SafeLink, Spinner, StatusBadge} from "./ui";
import {formatDate, plural, splitTags, timeAgo} from "../lib/format";
import {useDesktop} from "../lib/desktop";
import type {Requirement, RequirementTestLink, RequirementWrite, SourceType} from "../types";
import RequirementTests, {testLinkKey, useRequirementTests} from "./RequirementTests";

type EditMode = "amend" | "new-version";
type StatusFilter = "all" | "active" | "obsolete";

export const sourceTypes: {value: SourceType; label: string}[] = [
  {value: "ado", label: "Azure DevOps"},
  {value: "confluence", label: "Confluence"},
  {value: "github", label: "GitHub"},
  {value: "other", label: "Other"},
];

export default function RequirementsView() {
  const desktop = useDesktop();
  const {projectRef} = desktop;
  const requirements = useQuery({queryKey: ["requirements", projectRef], queryFn: () => api.listRequirements(projectRef)});
  const testLinks = useRequirementTests();
  const filterKey = `loomwork.requirementFilters.${projectRef}`;
  const [filters, setFilters] = useState(() => {
    try {
      const saved = JSON.parse(sessionStorage.getItem(filterKey) || "{}");
      return {search: typeof saved.search === "string" ? saved.search : "", status: (["all", "active", "obsolete"].includes(saved.status) ? saved.status : "all") as StatusFilter, tag: typeof saved.tag === "string" ? saved.tag : ""};
    } catch { return {search: "", status: "all" as StatusFilter, tag: ""}; }
  });
  const {search, status, tag} = filters;
  const setSearch = (search: string) => setFilters(current => ({...current, search}));
  const setStatus = (status: StatusFilter) => setFilters(current => ({...current, status}));
  const setTag = (tag: string) => setFilters(current => ({...current, tag}));
  useEffect(() => { sessionStorage.setItem(filterKey, JSON.stringify(filters)); }, [filterKey, filters]);
  const [editing, setEditing] = useState<{id: string; mode: EditMode} | null>(null);
  const [historyId, setHistoryId] = useState<string | null>(null);

  const allTags = useMemo(
    () => [...new Set((requirements.data || []).flatMap((requirement) => requirement.tags || []))].sort(),
    [requirements.data],
  );
  const visible = useMemo(() => {
    const needle = search.trim().toLowerCase();
    return (requirements.data || []).filter((requirement) =>
      (status === "all" || requirement.status === status)
      && (!tag || requirement.tags?.includes(tag))
      && (!needle || `${requirement.id} ${requirement.display_id || ""} ${requirement.text} ${requirement.source_ref || ""}`.toLowerCase().includes(needle)));
  }, [requirements.data, search, status, tag]);

  if (requirements.isLoading) return <div className="page-pad"><Spinner label="Loading requirements…" /></div>;
  if (requirements.error) return <div className="page-pad"><ErrorPanel error={requirements.error} /></div>;
  const total = requirements.data?.length || 0;
  const active = requirements.data?.filter((requirement) => requirement.status === "active").length || 0;

  return (
    <div className="list-view">
      <header className="list-head">
        <div>
          <h1>Requirements</h1>
          <p className="muted">{active} active · {total - active} obsolete. {desktop.requirementsReadOnly ? "Imported requirements are read-only. Filter by feature tag or search the original requirement ID." : "Each edit is either an in-place amend or a new retained version."}</p>
        </div>
        {!desktop.requirementsReadOnly && <button type="button" className="btn primary" onClick={() => desktop.openDialog("requirement")}><Icon.Plus size={14} /> New requirement</button>}
      </header>
      <div className="filters">
        <label className="search">
          <Icon.Search size={14} />
          <input value={search} onChange={(event) => setSearch(event.target.value)} placeholder="Search id, text, or source" />
        </label>
        <div className="segmented" role="group" aria-label="Status filter">
          {(["all", "active", "obsolete"] as StatusFilter[]).map((value) => (
            <button type="button" className={status === value ? "on" : ""} onClick={() => setStatus(value)} key={value}>{value}</button>
          ))}
        </div>
        {allTags.length > 0 && (
          <select value={tag} onChange={(event) => setTag(event.target.value)} aria-label="Tag filter">
            <option value="">All tags</option>
            {allTags.map((value) => <option value={value} key={value}>{value}</option>)}
          </select>
        )}
        <span className="muted small">{plural(visible.length, "match", "matches")}</span>
      </div>

      {total === 0 && (
        <EmptyState title="No requirements yet" icon={<Icon.Requirement size={26} />}>
          <p>Write requirements in tester-friendly language and cite the source document each one came from.</p>
          {!desktop.requirementsReadOnly && <button type="button" className="btn primary" onClick={() => desktop.openDialog("requirement")}><Icon.Plus size={14} /> Create the first requirement</button>}
        </EmptyState>
      )}
      {total > 0 && visible.length === 0 && <EmptyState title="No requirements match these filters" />}

      {testLinks.error && <ErrorPanel error={testLinks.error} />}
      <div className="req-list">
        {visible.map((requirement) => (
          <RequirementRow
            requirement={requirement}
            tests={testLinks.data?.[requirement.id]}
            testsLoading={testLinks.isPending}
            editing={editing?.id === requirement.id ? editing.mode : null}
            showHistory={historyId === requirement.id}
            onEdit={(mode) => { setHistoryId(null); setEditing(mode ? {id: requirement.id, mode} : null); }}
            onToggleHistory={() => { setEditing(null); setHistoryId((current) => current === requirement.id ? null : requirement.id); }}
            key={requirement.id}
          />
        ))}
      </div>
    </div>
  );
}

function RequirementRow({requirement, tests, testsLoading, editing, showHistory, onEdit, onToggleHistory}: {
  requirement: Requirement;
  tests?: RequirementTestLink[];
  testsLoading: boolean;
  editing: EditMode | null;
  showHistory: boolean;
  onEdit: (mode: EditMode | null) => void;
  onToggleHistory: () => void;
}) {
  const desktop = useDesktop();
  const queryClient = useQueryClient();
  const status = useMutation({
    mutationFn: (next: "active" | "obsolete") => api.setRequirementStatus(desktop.projectRef, requirement.id, next),
    onSuccess: () => {
      void queryClient.invalidateQueries({queryKey: ["requirement-history", desktop.projectRef, requirement.id]});
      desktop.refresh();
    },
  });
  const isActive = requirement.status === "active";
  return (
    <article className={`req-row ${editing || showHistory ? "expanded" : ""} ${isActive ? "" : "inactive"}`}>
      <div className="req-main">
        <div className="req-id-col">
          <button type="button" className="req-id" onClick={() => desktop.openItem({family: "requirements", ref: requirement.id, name: requirement.display_id || requirement.id})} title="Open requirement and tests">
            {requirement.display_id || requirement.id}
          </button>
          <small className="mono muted">v{requirement.version}</small>
        </div>
        <div className="req-body">
          <p className="req-text">{requirement.text}</p>
          <div className="req-meta">
            <StatusBadge status={requirement.status} />
            {requirement.metadata?.source_id && <Badge tone="info">{requirement.metadata.source_id}</Badge>}
            {requirement.metadata?.source_path && <button type="button" className="btn small" onClick={() => desktop.openItem({family: "artifacts", ref: requirement.metadata!.source_path, name: requirement.metadata!.source_path})}>Source snapshot</button>}
            {requirement.origin === "extracted" && <Badge tone="info" title="Extracted by document analysis">extracted</Badge>}
            {requirement.source_type && (
              <span className="req-source">
                <Badge>{requirement.source_type}</Badge>
                {requirement.source_ref && <SafeLink value={requirement.source_ref} className="truncate" />}
              </span>
            )}
            <Chips items={requirement.tags} />
            <span className="muted small" title={formatDate(requirement.created_at)}>{timeAgo(requirement.created_at)}</span>
          </div>
        </div>
        <div className="req-side">
          <RequirementTests links={tests} loading={testsLoading} compact onSelect={link => desktop.openItem({family: "requirements", ref: requirement.id, name: requirement.display_id || requirement.id}, testLinkKey(link))} />
          <div className="req-actions">
            {desktop.requirementsReadOnly ? <Badge>read-only</Badge> : <>
              <button type="button" className="btn small" onClick={() => onEdit(editing === "amend" ? null : "amend")} disabled={status.isPending} title="Rewrite this version in place">Edit</button>
              <button type="button" className="btn small" onClick={() => onEdit(editing === "new-version" ? null : "new-version")} disabled={status.isPending} title="Write the next version and retain this one">New version</button>
              <label className={`switch ${isActive ? "on" : ""}`} title={isActive ? "Active. Switch off to mark obsolete." : "Obsolete. Switch on to reactivate."}>
                <input type="checkbox" checked={isActive} onChange={event => status.mutate(event.currentTarget.checked ? "active" : "obsolete")} disabled={status.isPending || Boolean(editing)} aria-label={`${requirement.id} status: ${requirement.status}`} />
                <span className="switch-track" aria-hidden="true" /><span className="switch-label">{isActive ? "Active" : "Obsolete"}</span>
              </label>
            </>}
            <button type="button" className={`btn small ${showHistory ? "on" : ""}`} onClick={onToggleHistory} disabled={status.isPending}><Icon.History size={13} /> History</button>
          </div>
        </div>
      </div>
      {status.error && <ErrorPanel error={status.error} />}
      {editing && <RequirementEditor requirement={requirement} mode={editing} onDone={() => onEdit(null)} />}
      {showHistory && <RequirementHistory id={requirement.id} />}
    </article>
  );
}

// RequirementEditor edits text, tags, and source. Amend rewrites the current
// version in place; new-version writes the next version and supersedes this one.
export function RequirementEditor({requirement, mode, onDone}: {requirement: Requirement; mode: EditMode; onDone: () => void}) {
  const desktop = useDesktop();
  const queryClient = useQueryClient();
  const [text, setText] = useState(requirement.text);
  const [tags, setTags] = useState((requirement.tags || []).join(", "));
  const [sourceType, setSourceType] = useState<SourceType | "">(requirement.source_type || "");
  const [sourceRef, setSourceRef] = useState(requirement.source_ref || "");
  const save = useMutation({
    mutationFn: () => {
      const body: RequirementWrite = {text: text.trim(), tags: splitTags(tags)};
      if (sourceType) {
        body.source_type = sourceType;
        body.source_ref = sourceRef.trim();
      }
      return mode === "amend"
        ? api.amendRequirement(desktop.projectRef, requirement.id, body)
        : api.updateRequirement(desktop.projectRef, requirement.id, body);
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({queryKey: ["requirement-history", desktop.projectRef, requirement.id]});
      desktop.refresh();
      onDone();
    },
  });
  function submit(event: FormEvent) {
    event.preventDefault();
    save.mutate();
  }
  return (
    <form className="req-editor form" onSubmit={submit}>
      <div className="form-note">
        {mode === "amend"
          ? <><b>Edit in place.</b> Version {requirement.version} is rewritten; history is unchanged.</>
          : <><b>New version.</b> Version {requirement.version + 1} becomes active and v{requirement.version} is retained as superseded.</>}
      </div>
      <Field label="Requirement text">
        <textarea value={text} onChange={(event) => setText(event.target.value)} rows={4} required autoFocus />
      </Field>
      <div className="form-row">
        <Field label="Tags" hint="Comma-separated">
          <input value={tags} onChange={(event) => setTags(event.target.value)} />
        </Field>
        <Field label="Source type">
          <select value={sourceType} onChange={(event) => setSourceType(event.target.value as SourceType | "")}>
            <option value="">None</option>
            {sourceTypes.map((option) => <option value={option.value} key={option.value}>{option.label}</option>)}
          </select>
        </Field>
        <Field label="Source reference" hint="Story id, page URL, or file">
          <input value={sourceRef} onChange={(event) => setSourceRef(event.target.value)} disabled={!sourceType} placeholder={sourceType ? "AB#1234 or https://…" : "Choose a source type first"} />
        </Field>
      </div>
      {save.error && <ErrorPanel error={save.error} />}
      <div className="form-actions">
        <button type="button" className="btn" onClick={onDone} disabled={save.isPending}>Cancel</button>
        <button type="submit" className="btn primary" disabled={save.isPending || !text.trim()}>
          {save.isPending ? "Saving…" : mode === "amend" ? "Save" : "Save new version"}
        </button>
      </div>
    </form>
  );
}

// RequirementHistory lists every retained version, newest first.
export function RequirementHistory({id}: {id: string}) {
  const {projectRef} = useDesktop();
  const history = useQuery({
    queryKey: ["requirement-history", projectRef, id],
    queryFn: () => api.requirementHistory(projectRef, id),
  });
  if (history.isLoading) return <div className="req-history"><Spinner label="Loading history…" /></div>;
  if (history.error) return <div className="req-history"><ErrorPanel error={history.error} /></div>;
  const versions = [...(history.data || [])].sort((a, b) => b.version - a.version);
  return (
    <ol className="req-history">
      {versions.map((version) => (
        <li className="history-version" key={version.version}>
          <header>
            <b className="mono">v{version.version}</b>
            <StatusBadge status={version.status} />
            {version.source_type && <Badge>{version.source_type}</Badge>}
            <span className="muted small">{formatDate(version.created_at)}</span>
          </header>
          <p>{version.text}</p>
          {version.tags?.length ? <Chips items={version.tags} /> : null}
        </li>
      ))}
    </ol>
  );
}
