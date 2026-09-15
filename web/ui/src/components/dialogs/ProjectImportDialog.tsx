import {FormEvent, useState} from "react";
import {useMutation, useQueryClient} from "@tanstack/react-query";
import {useNavigate} from "react-router-dom";
import {api} from "../../api";
import {Dialog, ErrorPanel, Field} from "../ui";

export default function ProjectImportDialog({onClose}: {onClose: () => void}) {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [path, setPath] = useState("");
  const [name, setName] = useState("");
  const [features, setFeatures] = useState<string[]>([]);
  const preview = useMutation({
    mutationFn: () => api.previewProjectImport({format: "spec-kit", path: path.trim()}),
    onSuccess: result => {
      if (!name.trim()) setName(result.name);
      setFeatures(result.features.map(feature => feature.id));
    },
  });
  const save = useMutation({
    mutationFn: () => api.importProject({format: "spec-kit", path: path.trim(), name: name.trim(), features}),
    onSuccess: async result => {
      await queryClient.invalidateQueries({queryKey: ["projects"]});
      onClose();
      navigate(`/projects/${encodeURIComponent(result.project.id)}`);
    },
  });
  const busy = preview.isPending || save.isPending;
  const selected = preview.data?.features.filter(feature => features.includes(feature.id)) || [];
  function submit(event: FormEvent) {
    event.preventDefault();
    if (preview.data) save.mutate();
    else preview.mutate();
  }
  return (
    <Dialog open title="Import project" onClose={onClose} width={760}>
      <form className="form" onSubmit={submit}>
        <div className="form-note">Import a local snapshot, not a live link. Requirements stay read-only. QA folders appear under Test Suites as editable document suites; native suite JSON and reports can be added separately. Nothing writes back to the source.</div>
        <Field label="Project format"><select disabled><option>spec-kit</option></select></Field>
        <Field label="Source directory" hint="Absolute path on the machine running Loomwork, containing .specify/ and specs/.">
          <input value={path} onChange={event => {setPath(event.target.value); preview.reset(); save.reset();}} placeholder="/home/you/projects/service" required disabled={busy} autoFocus />
        </Field>
        {preview.error && <ErrorPanel error={preview.error} />}
        {preview.data && <>
          <Field label="Local project name" hint="Must be unique in this workspace."><input value={name} onChange={event => setName(event.target.value)} required disabled={busy} /></Field>
          <fieldset className="fieldset">
            <legend>Features to import</legend>
            <label className="check"><input type="checkbox" checked={features.length === preview.data.features.length} disabled={busy} onChange={event => setFeatures(event.target.checked ? preview.data!.features.map(feature => feature.id) : [])} />Select all</label>
            <div className="import-feature-list">
              {preview.data.features.map(feature => <label className="import-feature" key={feature.id}>
                <input type="checkbox" checked={features.includes(feature.id)} disabled={busy} onChange={event => setFeatures(current => event.target.checked ? [...current, feature.id] : current.filter(id => id !== feature.id))} />
                <span><b>{feature.title}</b><code className="muted small">{feature.id}</code><span className="muted small">{feature.requirements} requirements · {feature.artifacts} documents · {feature.testDocuments} test documents</span></span>
              </label>)}
            </div>
          </fieldset>
          <p className="muted small">{selected.length} features selected · {selected.reduce((n, feature) => n + feature.requirements, 0)} read-only requirements. IDs combine the spec prefix and original FR, NFR, or SC number, such as 008-FR-001; they do not depend on import order.</p>
          {preview.data.warnings.length > 0 && <details><summary>{preview.data.warnings.length} import warnings</summary><ul>{preview.data.warnings.map(warning => <li key={warning}>{warning}</li>)}</ul></details>}
        </>}
        {save.error && <ErrorPanel error={save.error} />}
        <div className="form-actions">
          <button type="button" className="btn" onClick={onClose} disabled={busy}>Cancel</button>
          <button type="submit" className="btn primary" disabled={busy || !path.trim() || Boolean(preview.data && (!features.length || !name.trim()))}>
            {preview.isPending ? "Reading project…" : save.isPending ? "Importing…" : preview.data ? "Import snapshot" : "Preview import"}
          </button>
        </div>
      </form>
    </Dialog>
  );
}
