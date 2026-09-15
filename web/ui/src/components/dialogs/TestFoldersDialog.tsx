import {FormEvent, useEffect, useState} from "react";
import {useMutation, useQuery, useQueryClient} from "@tanstack/react-query";
import {api} from "../../api";
import {useDesktop} from "../../lib/desktop";
import {Dialog, ErrorPanel, Field, Spinner} from "../ui";

export default function TestFoldersDialog({onClose}: {onClose: () => void}) {
  const desktop = useDesktop();
  const queryClient = useQueryClient();
  const settings = useQuery({queryKey: ["test-document-settings", desktop.projectRef], queryFn: () => api.testDocumentSettings(desktop.projectRef)});
  const [roots, setRoots] = useState("");
  useEffect(() => { if (settings.data) setRoots(settings.data.roots.join("\n")); }, [settings.data]);
  const save = useMutation({
    mutationFn: () => api.setTestDocumentSettings(desktop.projectRef, roots.split("\n").map(root => root.trim()).filter(Boolean)),
    onSuccess: result => {
      queryClient.setQueryData(["test-document-settings", desktop.projectRef], result);
      desktop.refresh();
      onClose();
    },
  });
  function submit(event: FormEvent) { event.preventDefault(); save.mutate(); }
  return <Dialog open title="Configure test folders" onClose={onClose} width={660}>
    <form className="form" onSubmit={submit}>
      <div className="form-note">Each folder becomes a document suite under Test Suites. This only changes grouping in Loomwork: source files, document history, and requirement links remain unchanged.</div>
      {settings.isPending && <Spinner />}
      {settings.error && <ErrorPanel error={settings.error} />}
      <Field label="Test folder roots" hint="Project-relative paths, one per line, without wildcards. Leave empty to show all documents in Artifacts.">
        <textarea rows={8} className="mono" value={roots} onChange={event => setRoots(event.target.value)} disabled={!settings.data || save.isPending} placeholder="specs/008-feature/sdd-qa" />
      </Field>
      <p className="muted small">Only documents already copied into this project are grouped; this setting does not read new files from the source repository.</p>
      {save.error && <ErrorPanel error={save.error} />}
      <div className="form-actions">
        <button type="button" className="btn" onClick={() => setRoots(settings.data?.defaults.join("\n") || "")} disabled={!settings.data || save.isPending}>Use import defaults</button>
        <button type="button" className="btn" onClick={onClose} disabled={save.isPending}>Cancel</button>
        <button type="submit" className="btn primary" disabled={!settings.data || save.isPending}>{save.isPending ? "Saving…" : "Save folders"}</button>
      </div>
    </form>
  </Dialog>;
}
