import {useState} from "react";
import {useMutation, useQueryClient} from "@tanstack/react-query";
import type {ViewerProps} from ".";
import {api} from "../../api";
import {Icon} from "../Icons";
import {Badge, Chips, ErrorPanel, KeyValue, SafeLink, StatusBadge} from "../ui";
import {formatDate} from "../../lib/format";
import {useDesktop} from "../../lib/desktop";
import {RequirementEditor, RequirementHistory} from "../RequirementsView";
import type {Requirement} from "../../types";

export default function RequirementViewer({document, readOnly}: ViewerProps) {
  const desktop = useDesktop();
  const queryClient = useQueryClient();
  const requirement = document.body as Requirement;
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
    <div className="entity">
      <header className="entity-head">
        <h2><code>{requirement.id}</code></h2>
        <StatusBadge status={requirement.status} />
        {requirement.origin === "extracted" && <Badge tone="info">extracted</Badge>}
        <span className="spacer" />
        {!readOnly && (
          <div className="entity-actions">
            <button type="button" className={`btn small ${editing === "amend" ? "on" : ""}`} onClick={() => setEditing(editing === "amend" ? null : "amend")}>Edit</button>
            <button type="button" className={`btn small ${editing === "new-version" ? "on" : ""}`} onClick={() => setEditing(editing === "new-version" ? null : "new-version")}>New version</button>
            {requirement.status !== "superseded" && (
              <button type="button" className="btn small" onClick={() => status.mutate(isActive ? "obsolete" : "active")} disabled={status.isPending}>
                {isActive ? "Mark obsolete" : "Reactivate"}
              </button>
            )}
          </div>
        )}
      </header>
      {status.error && <ErrorPanel error={status.error} />}
      {editing && !readOnly
        ? <RequirementEditor requirement={requirement} mode={editing} onDone={() => setEditing(null)} />
        : <blockquote className="req-quote">{requirement.text}</blockquote>}
      <KeyValue rows={[
        ["Version", `v${requirement.version}`],
        ["Created", formatDate(requirement.created_at)],
        ["Origin", requirement.origin],
        ["Source", requirement.source_type ? <span key="src" className="req-source"><Badge>{requirement.source_type}</Badge>{requirement.source_ref && <SafeLink value={requirement.source_ref} />}</span> : ""],
        ["Tags", requirement.tags?.length ? <Chips key="tags" items={requirement.tags} /> : ""],
        ["Metadata", requirement.metadata && Object.keys(requirement.metadata).length
          ? <pre key="meta" className="inline-code">{JSON.stringify(requirement.metadata, null, 2)}</pre> : ""],
      ]} />
      <section>
        <h3><Icon.History size={14} /> Version history</h3>
        <RequirementHistory id={requirement.id} />
      </section>
    </div>
  );
}
