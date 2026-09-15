import {FormEvent, useState} from "react";
import {useMutation} from "@tanstack/react-query";
import {api} from "../../api";
import {Dialog, ErrorPanel, Field} from "../ui";
import {sourceTypes} from "../RequirementsView";
import {useDesktop} from "../../lib/desktop";
import type {DocumentSource, SourceType} from "../../types";

export default function SourceDialog({open, onClose, sources}: {open: boolean; onClose: () => void; sources: DocumentSource[]}) {
  const desktop = useDesktop();
  const [name, setName] = useState("");
  const [type, setType] = useState<SourceType>("github");
  const [url, setUrl] = useState("");
  const add = useMutation({
    mutationFn: () => api.addSource(desktop.projectRef, {name: name.trim(), type, url: url.trim()}),
    onSuccess: () => {
      setName("");
      setUrl("");
      desktop.refresh();
      onClose();
    },
  });
  const replaces = sources.find((source) => source.name.toLowerCase() === name.trim().toLowerCase());
  function submit(event: FormEvent) {
    event.preventDefault();
    add.mutate();
  }
  return (
    <Dialog open={open} title="Link a documentation source" onClose={onClose} width={520}>
      <form className="form" onSubmit={submit}>
        <Field label="Name" hint={replaces ? `Replaces the existing "${replaces.name}" link.` : "Short handle requirements can cite, e.g. spec, stories, repo."}>
          <input value={name} onChange={(event) => setName(event.target.value)} required autoFocus placeholder="spec" />
        </Field>
        <Field label="Type">
          <select value={type} onChange={(event) => setType(event.target.value as SourceType)}>
            {sourceTypes.map((option) => <option value={option.value} key={option.value}>{option.label}</option>)}
          </select>
        </Field>
        <Field label="URL" hint="Only http(s) links render as clickable.">
          <input value={url} onChange={(event) => setUrl(event.target.value)} type="url" required placeholder="https://wiki.example.com/checkout" />
        </Field>
        {add.error && <ErrorPanel error={add.error} />}
        <div className="form-actions">
          <button type="button" className="btn" onClick={onClose}>Cancel</button>
          <button type="submit" className="btn primary" disabled={add.isPending || !name.trim() || !url.trim()}>{add.isPending ? "Linking…" : replaces ? "Replace link" : "Link source"}</button>
        </div>
      </form>
    </Dialog>
  );
}
