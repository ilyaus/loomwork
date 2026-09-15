import {FormEvent, useEffect, useState} from "react";
import {useMutation} from "@tanstack/react-query";
import {api} from "../../api";
import {Dialog, ErrorPanel, Field} from "../ui";
import {splitTags} from "../../lib/format";
import {useDesktop} from "../../lib/desktop";
import type {AgentDefinition, AgentTarget} from "../../types";

const defaultBody = `# Role
You generate REST API test suites from an OpenAPI spec, requirements, and override rules.
Override rules take precedence over the literal spec whenever they conflict.

# Override rule handling
- Treat each override rule as a correction to expected behavior, not a suggestion.
- When the spec and a rule conflict, follow the rule and cite it in overrides_applied.
- When no rule addresses an ambiguity, default to the spec and flag it as an open question.
`;

// AgentDialog creates a definition or, when existing is given, writes its next
// version with the fields pre-filled.
export default function AgentDialog({open, onClose, existing}: {open: boolean; onClose: () => void; existing?: AgentDefinition}) {
  const desktop = useDesktop();
  const [name, setName] = useState("");
  const [target, setTarget] = useState<AgentTarget>("claude-agent-sdk");
  const [model, setModel] = useState("");
  const [tools, setTools] = useState("read_swagger, read_requirements, read_override_rules, write_test_file");
  const [description, setDescription] = useState("");
  const [body, setBody] = useState(defaultBody);
  const [tags, setTags] = useState("");

  useEffect(() => {
    if (!open) return;
    setName(existing?.agent_name || "");
    setTarget(existing?.target_provider || "claude-agent-sdk");
    setModel(existing?.model || "");
    setTools(existing?.tools_allowed?.join(", ") ?? "read_swagger, read_requirements, read_override_rules, write_test_file");
    setDescription(existing?.description || "");
    setBody(existing?.body || defaultBody);
    setTags(existing?.tags?.join(", ") || "");
  }, [open, existing]);

  const save = useMutation({
    mutationFn: () => {
      const write = {
        target_provider: target, model: model.trim(), tools_allowed: splitTags(tools),
        body: body.trim(), description: description.trim(), tags: splitTags(tags),
      };
      return existing
        ? api.updateAgentDefinition(desktop.projectRef, existing.agent_name, write)
        : api.createAgentDefinition(desktop.projectRef, {agent_name: name.trim(), ...write});
    },
    onSuccess: (definition) => {
      desktop.refresh();
      onClose();
      desktop.openItem({family: "agent-definitions", ref: definition.agent_name, name: definition.agent_name, version: definition.version, artifactType: "agent-definition"});
    },
  });
  function submit(event: FormEvent) {
    event.preventDefault();
    save.mutate();
  }

  return (
    <Dialog open={open} title={existing ? `New version of ${existing.agent_name}` : "New agent definition"} onClose={onClose} width={720}>
      <form className="form" onSubmit={submit}>
        {existing && <div className="form-note">Version {existing.version + 1} will become current. Version {existing.version} stays on disk unchanged.</div>}
        <div className="form-row">
          <Field label="Agent name" hint="Lowercase letters, digits, and dashes; also the file name stem.">
            <input value={name} onChange={(event) => setName(event.target.value)} pattern="[a-z0-9][a-z0-9\-]*" placeholder="rest-api-test-generator" required disabled={Boolean(existing)} autoFocus={!existing} />
          </Field>
          <Field label="Target adapter">
            <select value={target} onChange={(event) => setTarget(event.target.value as AgentTarget)}>
              <option value="claude-agent-sdk">Claude Agent SDK</option>
              <option value="copilot-sdk">Copilot SDK (declared, no adapter yet)</option>
            </select>
          </Field>
          <Field label="Model" hint="Optional; the run can override it.">
            <input value={model} onChange={(event) => setModel(event.target.value)} placeholder="claude-sonnet-4" />
          </Field>
        </div>
        <Field label="Description">
          <input value={description} onChange={(event) => setDescription(event.target.value)} placeholder="Generates REST API test suites from the OpenAPI spec and requirements" />
        </Field>
        <Field label="Tools allowed" hint="Comma-separated strict allowlist. A definition that names no tools grants none.">
          <input value={tools} onChange={(event) => setTools(event.target.value)} className="mono" />
        </Field>
        <Field label="Instructions (Markdown)">
          <textarea value={body} onChange={(event) => setBody(event.target.value)} rows={14} className="mono" required spellCheck={false} />
        </Field>
        <Field label="Tags" hint="Comma-separated">
          <input value={tags} onChange={(event) => setTags(event.target.value)} />
        </Field>
        {save.error && <ErrorPanel error={save.error} />}
        <div className="form-actions">
          <button type="button" className="btn" onClick={onClose}>Cancel</button>
          <button type="submit" className="btn primary" disabled={save.isPending || !body.trim() || (!existing && !name.trim())}>
            {save.isPending ? "Saving…" : existing ? "Save new version" : "Create definition"}
          </button>
        </div>
      </form>
    </Dialog>
  );
}
