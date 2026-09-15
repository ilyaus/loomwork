import {useEffect, useState} from "react";
import {useMutation, useQueryClient} from "@tanstack/react-query";
import {ItemViewer, type ViewerProps} from ".";
import {api} from "../../api";
import {Icon} from "../Icons";
import {Badge, Chips, EmptyState, ErrorPanel, KeyValue, SafeLink, Spinner, StatusBadge} from "../ui";
import {baseName, dirName, formatDate} from "../../lib/format";
import {useDesktop} from "../../lib/desktop";
import {RequirementEditor, RequirementHistory} from "../RequirementsView";
import type {Requirement} from "../../types";
import {testLinkKey, useRequirementTests} from "../RequirementTests";

export default function RequirementViewer({document, readOnly, selectedTest}: ViewerProps) {
  const desktop = useDesktop();
  const queryClient = useQueryClient();
  const requirement = document.body as Requirement;
  const name = requirement.display_id || requirement.id;
  const tests = useRequirementTests();
  const [selection, setSelection] = useState(selectedTest);
  useEffect(() => { setSelection(selectedTest); }, [selectedTest]);
  const links = tests.data?.[requirement.id] || [];
  const selected = links.find(link => testLinkKey(link) === selection) || links[0];
  const [editing, setEditing] = useState<"amend" | "new-version" | null>(null);
  const status = useMutation({
    mutationFn: (next: "active" | "obsolete") => api.setRequirementStatus(desktop.projectRef, requirement.id, next),
    onSuccess: () => {
      void queryClient.invalidateQueries({queryKey: ["requirement-history", desktop.projectRef, requirement.id]});
      desktop.refresh();
    },
  });
  const isActive = requirement.status === "active";

  return (
    <div className="requirement-workbench">
      <div className="requirement-summary">
        <button type="button" className="btn small" onClick={() => desktop.openFamily("requirements")}><Icon.Chevron size={12} className="back-chevron" /> Back to requirements</button>
        <header className="entity-head">
          <h2><code>{name}</code></h2>
          <StatusBadge status={requirement.status} />
          {requirement.origin === "extracted" && <Badge tone="info">extracted</Badge>}
          <span className="spacer" />
          {!readOnly && <div className="entity-actions">
            <button type="button" className="btn small" onClick={() => setEditing(editing === "amend" ? null : "amend")}>Edit</button>
            <button type="button" className="btn small" onClick={() => setEditing(editing === "new-version" ? null : "new-version")}>New version</button>
            {requirement.status !== "superseded" && <button type="button" className="btn small" onClick={() => status.mutate(isActive ? "obsolete" : "active")} disabled={status.isPending}>{isActive ? "Mark obsolete" : "Reactivate"}</button>}
          </div>}
        </header>
        {status.error && <ErrorPanel error={status.error} />}
        {editing && !readOnly
          ? <RequirementEditor requirement={requirement} mode={editing} onDone={() => setEditing(null)} />
          : <blockquote className="req-quote">{requirement.text}</blockquote>}
      </div>
      <section className="requirement-test-section">
        <header className="requirement-test-toolbar">
          <h3>Tests for {name} <Badge>{links.length}</Badge></h3>
          {selected && <>
            <select aria-label="Linked test" value={testLinkKey(selected)} onChange={event => setSelection(event.target.value)} title={selected.name}>
              {links.map(link => <option key={testLinkKey(link)} value={testLinkKey(link)}>{link.family === "artifacts" ? baseName(link.name) : link.name} · v{link.version}{link.family === "artifacts" ? ` · ${dirName(link.name)}` : ""}</option>)}
            </select>
            <button type="button" className="btn small" onClick={() => desktop.openItem(selected)}>Open test separately</button>
          </>}
        </header>
        {tests.isPending ? <Spinner label="Loading linked tests…" /> : tests.error ? <ErrorPanel error={tests.error} /> : selected ? (
          <div className="requirement-test-preview"><ItemViewer embedded family={selected.family} ref_={selected.ref} key={`${selected.family}:${selected.ref}`} /></div>
        ) : <EmptyState title="No linked tests">Link a native case or a scenario document to this requirement.</EmptyState>}
      </section>
      <details className="requirement-provenance">
        <summary>Source and version history</summary>
        <KeyValue rows={[
          ["Reference", name],
          ["Legacy storage ID", requirement.display_id ? requirement.id : ""],
          ["Version", `v${requirement.version}`],
          ["Created", formatDate(requirement.created_at)],
          ["Origin", requirement.origin],
          ["Source", requirement.source_type ? <span key="src" className="req-source"><Badge>{requirement.source_type}</Badge>{requirement.source_ref && <SafeLink value={requirement.source_ref} />}</span> : ""],
          ["Tags", requirement.tags?.length ? <Chips key="tags" items={requirement.tags} /> : ""],
          ["Metadata", requirement.metadata && <pre key="meta" className="inline-code">{JSON.stringify(requirement.metadata, null, 2)}</pre>],
        ]} />
        <RequirementHistory id={requirement.id} />
      </details>
    </div>
  );
}
