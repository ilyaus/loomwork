import {ChangeEvent, FormEvent, useState} from "react";
import {useMutation} from "@tanstack/react-query";
import {api} from "../../api";
import {Dialog, ErrorPanel, Field} from "../ui";
import {splitTags} from "../../lib/format";
import {useDesktop} from "../../lib/desktop";
import type {Artifact, ArtifactType, TreeItem} from "../../types";

const artifactTypes: {value: ArtifactType; label: string}[] = [
  {value: "spec", label: "Spec (OpenAPI, scenarios)"},
  {value: "doc", label: "Doc (notes, exported pages)"},
  {value: "log", label: "Log"},
  {value: "test-result", label: "Test result"},
  {value: "diagram", label: "Diagram"},
  {value: "generated", label: "Generated"},
];

const maxBytes = 6 * 1024 * 1024;

export default function ArtifactDialog({onClose, existing, report = false}: {onClose: () => void; existing?: Artifact; report?: boolean}) {
  const desktop = useDesktop();
  const [name, setName] = useState(existing?.name || "");
  const [type, setType] = useState<ArtifactType>(existing?.type || "doc");
  const [content, setContent] = useState(existing?.body.content || "");
  const [mediaType, setMediaType] = useState(existing?.body.mediaType || "");
  const [tags, setTags] = useState(existing?.tags?.join(", ") || "");
  const [pinned, setPinned] = useState(existing?.pinned || false);
  const [fileError, setFileError] = useState("");
  const [reading, setReading] = useState(false);
  const title = report ? "Add report" : existing ? `New version of ${existing.name}` : "Add artifact";
  const add = useMutation({
    mutationFn: async (): Promise<Pick<TreeItem, "family" | "ref" | "name">> => {
      if (new TextEncoder().encode(content).length > maxBytes) throw new Error("Content exceeds the 6 MB limit.");
      if (report) {
        const saved = await api.addReport(desktop.projectRef, {name: name.trim(), content});
        return {family: "reports", ref: saved.name, name: saved.name};
      }
      const saved = await api.addArtifact(desktop.projectRef, {
        name: name.trim(), type, content, mediaType: mediaType.trim() || undefined, tags: splitTags(tags), pinned,
      });
      return {family: "artifacts", ref: saved.id, name: saved.name};
    },
    onSuccess: (item) => {
      desktop.refresh();
      onClose();
      desktop.openItem(item);
    },
  });

  async function pickFile(event: ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0];
    event.target.value = "";
    if (!file) return;
    setFileError("");
    if (file.size > maxBytes) {
      setFileError(`${file.name} exceeds the 6 MB limit.`);
      return;
    }
    setReading(true);
    try {
      const text = new TextDecoder("utf-8", {fatal: true}).decode(await file.arrayBuffer());
      if (text.includes("\0")) throw new Error("Choose a UTF-8 text file, not a binary file.");
      setContent(text);
      if (!existing) setName(file.name);
      if (!existing) setMediaType(/\.md$/i.test(file.name) ? "text/markdown" : file.type);
    } catch (error) {
      setFileError(error instanceof Error ? error.message : "Could not read the file.");
    } finally {
      setReading(false);
    }
  }

  function submit(event: FormEvent) {
    event.preventDefault();
    add.mutate();
  }

  return (
    <Dialog open title={title} onClose={onClose} width={640}>
      <form className="form" onSubmit={submit}>
        {report && <div className="form-note">Reports are append-only execution results, separate from artifacts. Use a unique file name for each run.</div>}
        {existing && <div className="form-note">Save a new local version. Earlier versions are retained, and the original source project is never changed.</div>}
        <div className="form-row">
          <Field label="Name" hint={report ? "Relative path, such as suite-orders/v1/run.json" : "Reusing a name adds the next revision of that artifact."}>
            <input value={name} onChange={event => setName(event.target.value)} placeholder={report ? "suite-orders/v1/run.json" : "notes.md"} required disabled={Boolean(existing)} autoFocus={!existing} />
          </Field>
          {!report && <Field label="Type">
            <select value={type} onChange={event => setType(event.target.value as ArtifactType)} disabled={Boolean(existing)}>
              {artifactTypes.map(option => <option value={option.value} key={option.value}>{option.label}</option>)}
            </select>
          </Field>}
        </div>
        <Field label="Content" hint="Paste text or choose a UTF-8 file, up to 6 MB.">
          <textarea value={content} onChange={event => setContent(event.target.value)} rows={10} className="mono" required />
        </Field>
        <div className="form-row">
          <label className="btn file-btn">Choose file…<input type="file" onChange={event => void pickFile(event)} hidden disabled={reading || add.isPending} /></label>
          <span className="muted small">{reading ? "Reading file…" : `${content.length.toLocaleString()} characters`}</span>
        </div>
        {fileError && <ErrorPanel error={fileError} />}
        {!report && <>
          <div className="form-row">
            <Field label="Media type" hint="Optional. Inferred from the extension when empty."><input value={mediaType} onChange={event => setMediaType(event.target.value)} placeholder="text/markdown" /></Field>
            <Field label="Tags" hint="Comma-separated"><input value={tags} onChange={event => setTags(event.target.value)} /></Field>
          </div>
          <label className="check"><input type="checkbox" checked={pinned} onChange={event => setPinned(event.target.checked)} /><span>Pin as standing context for prompt runs</span></label>
        </>}
        {add.error && <ErrorPanel error={add.error} />}
        <div className="form-actions">
          <button type="button" className="btn" onClick={onClose} disabled={add.isPending}>Cancel</button>
          <button type="submit" className="btn primary" disabled={add.isPending || reading || !name.trim() || !content}>{add.isPending ? "Saving…" : existing ? "Save new version" : title}</button>
        </div>
      </form>
    </Dialog>
  );
}
