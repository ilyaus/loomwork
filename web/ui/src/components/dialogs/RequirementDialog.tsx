import {FormEvent, useState} from "react";
import {useMutation} from "@tanstack/react-query";
import {api} from "../../api";
import {Dialog, ErrorPanel, Field} from "../ui";
import {sourceTypes} from "../RequirementsView";
import {splitTags} from "../../lib/format";
import {useDesktop} from "../../lib/desktop";
import type {RequirementWrite, SourceType} from "../../types";

export default function RequirementDialog({open, onClose}: {open: boolean; onClose: () => void}) {
  const desktop = useDesktop();
  const [text, setText] = useState("");
  const [tags, setTags] = useState("");
  const [sourceType, setSourceType] = useState<SourceType | "">("");
  const [sourceRef, setSourceRef] = useState("");
  const [origin, setOrigin] = useState<"authored" | "extracted">("authored");
  const create = useMutation({
    mutationFn: () => {
      const body: RequirementWrite = {text: text.trim(), tags: splitTags(tags), origin};
      if (sourceType) {
        body.source_type = sourceType;
        body.source_ref = sourceRef.trim();
      }
      return api.createRequirement(desktop.projectRef, body);
    },
    onSuccess: (requirement) => {
      setText("");
      setSourceRef("");
      desktop.refresh();
      onClose();
      desktop.openItem({family: "requirements", ref: requirement.id, name: requirement.id});
    },
  });
  function submit(event: FormEvent) {
    event.preventDefault();
    create.mutate();
  }
  return (
    <Dialog open={open} title="New requirement" onClose={onClose} width={620}>
      <form className="form" onSubmit={submit}>
        <Field label="Requirement" hint="Tester-friendly language: what must be true, observable from outside the service.">
          <textarea value={text} onChange={(event) => setText(event.target.value)} rows={5} required autoFocus placeholder="GET /orders/{id} for an unknown order returns 200 with an empty list." />
        </Field>
        <div className="form-row">
          <Field label="Source type">
            <select value={sourceType} onChange={(event) => setSourceType(event.target.value as SourceType | "")}>
              <option value="">None</option>
              {sourceTypes.map((option) => <option value={option.value} key={option.value}>{option.label}</option>)}
            </select>
          </Field>
          <Field label="Source reference" hint="Story id, page URL, or document path">
            <input value={sourceRef} onChange={(event) => setSourceRef(event.target.value)} disabled={!sourceType} placeholder={sourceType ? "AB#1234 or https://…" : "Choose a source type first"} />
          </Field>
        </div>
        <div className="form-row">
          <Field label="Tags" hint="Comma-separated">
            <input value={tags} onChange={(event) => setTags(event.target.value)} placeholder="orders, validation" />
          </Field>
          <Field label="Origin">
            <select value={origin} onChange={(event) => setOrigin(event.target.value as "authored" | "extracted")}>
              <option value="authored">Authored by QA</option>
              <option value="extracted">Extracted from a document</option>
            </select>
          </Field>
        </div>
        {create.error && <ErrorPanel error={create.error} />}
        <div className="form-actions">
          <button type="button" className="btn" onClick={onClose}>Cancel</button>
          <button type="submit" className="btn primary" disabled={create.isPending || !text.trim()}>{create.isPending ? "Creating…" : "Create requirement"}</button>
        </div>
      </form>
    </Dialog>
  );
}
