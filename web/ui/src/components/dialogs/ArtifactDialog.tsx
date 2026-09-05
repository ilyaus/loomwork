import {ChangeEvent, FormEvent, useState} from "react";
import {useMutation} from "@tanstack/react-query";
import {api} from "../../api";
import {Dialog, ErrorPanel, Field} from "../ui";
import {splitTags} from "../../lib/format";
import {useDesktop} from "../../lib/desktop";
import type {ArtifactType} from "../../types";

const artifactTypes: {value: ArtifactType; label: string}[] = [
  {value: "spec", label: "Spec (OpenAPI, scenarios)"},
  {value: "doc", label: "Doc (notes, exported pages)"},
  {value: "log", label: "Log"},
  {value: "test-result", label: "Test result"},
  {value: "diagram", label: "Diagram"},
  {value: "generated", label: "Generated"},
];

const maxBytes = 6 * 1024 * 1024;

export default function ArtifactDialog({open, onClose}: {open: boolean; onClose: () => void}) {
  const desktop = useDesktop();
  const [name, setName] = useState("");
  const [type, setType] = useState<ArtifactType>("doc");
  const [content, setContent] = useState("");
  const [mediaType, setMediaType] = useState("");
  const [tags, setTags] = useState("");
  const [pinned, setPinned] = useState(false);
  const [fileError, setFileError] = useState("");
  const add = useMutation({
    mutationFn: () => api.addArtifact(desktop.projectRef, {
      name: name.trim(), type, content, mediaType: mediaType.trim() || undefined, tags: splitTags(tags), pinned,
    }),
    onSuccess: (artifact) => {
      setName("");
      setContent("");
      setMediaType("");
      desktop.refresh();
      onClose();
      desktop.openItem({family: "artifacts", ref: artifact.id, name: artifact.name, version: artifact.version, artifactType: artifact.type});
    },
  });

  function pickFile(event: ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0];
    if (!file) return;
    if (file.size > maxBytes) {
      setFileError(`${file.name} is ${(file.size / 1024 / 1024).toFixed(1)} MB; the limit is 6 MB.`);
      return;
    }
    setFileError("");
    void file.text().then((text) => {
      setContent(text);
      if (!name.trim()) setName(file.name);
      if (file.type && !mediaType) setMediaType(file.type);
      if (/\.(ya?ml|json)$/i.test(file.name) && type === "doc") setType("spec");
      if (/\.log$/i.test(file.name)) setType("log");
    });
  }

  function submit(event: FormEvent) {
    event.preventDefault();
    add.mutate();
  }

  return (
    <Dialog open={open} title="Add artifact" onClose={onClose} width={640}>
      <form className="form" onSubmit={submit}>
        <div className="form-row">
          <Field label="Name" hint="Re-using a name adds the next revision of that artifact.">
            <input value={name} onChange={(event) => setName(event.target.value)} placeholder="openapi.json" required autoFocus />
          </Field>
          <Field label="Type">
            <select value={type} onChange={(event) => setType(event.target.value as ArtifactType)}>
              {artifactTypes.map((option) => <option value={option.value} key={option.value}>{option.label}</option>)}
            </select>
          </Field>
        </div>
        <Field label="Content" hint="Paste text or choose a file; the file's text is stored inline in the project.">
          <textarea value={content} onChange={(event) => setContent(event.target.value)} rows={10} className="mono" placeholder="Paste content here…" />
        </Field>
        <div className="form-row">
          <label className="btn file-btn">
            Choose file…
            <input type="file" onChange={pickFile} hidden />
          </label>
          <span className="muted small">{content ? `${content.length.toLocaleString()} characters` : "No content yet"}</span>
        </div>
        {fileError && <ErrorPanel error={fileError} />}
        <div className="form-row">
          <Field label="Media type" hint="Optional. Inferred from the file extension when empty.">
            <input value={mediaType} onChange={(event) => setMediaType(event.target.value)} placeholder="text/markdown" />
          </Field>
          <Field label="Tags" hint="Comma-separated">
            <input value={tags} onChange={(event) => setTags(event.target.value)} />
          </Field>
        </div>
        <label className="check">
          <input type="checkbox" checked={pinned} onChange={(event) => setPinned(event.target.checked)} />
          <span>Pin as standing context for prompt runs</span>
        </label>
        {add.error && <ErrorPanel error={add.error} />}
        <div className="form-actions">
          <button type="button" className="btn" onClick={onClose}>Cancel</button>
          <button type="submit" className="btn primary" disabled={add.isPending || !name.trim() || !content}>{add.isPending ? "Adding…" : "Add artifact"}</button>
        </div>
      </form>
    </Dialog>
  );
}
