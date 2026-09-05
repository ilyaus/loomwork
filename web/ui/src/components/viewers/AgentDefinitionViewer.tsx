import {useMemo, useState} from "react";
import type {ViewerProps} from ".";
import {Badge, Chips, KeyValue} from "../ui";
import {Markdown} from "../Markdown";
import {formatDate} from "../../lib/format";
import {useDesktop} from "../../lib/desktop";
import type {AgentDefinition} from "../../types";
import {TextViewer, bodyText} from "./TextViewer";

// parseDefinition reads the on-disk form: a small key/value frontmatter block
// (scalars, [bracketed, lists], metadata.<key>) followed by the markdown body.
export function parseDefinition(source: string): AgentDefinition | null {
  const trimmed = source.replace(/^\uFEFF/, "").trimStart();
  if (!trimmed.startsWith("---")) return null;
  const rest = trimmed.slice(3).replace(/^\r?\n/, "");
  const end = rest.indexOf("\n---");
  if (end < 0) return null;
  const frontmatter = rest.slice(0, end);
  const body = rest.slice(end + 4).replace(/^[^\n]*\n?/, "").trim();
  const list = (value: string) => value.replace(/^\[/, "").replace(/\]$/, "").split(",").map((part) => part.trim().replace(/^['"]|['"]$/g, "")).filter(Boolean);
  const definition: AgentDefinition = {agent_name: "", version: 0, target_provider: "claude-agent-sdk", body, created_at: ""};
  for (const line of frontmatter.split("\n")) {
    const separator = line.indexOf(":");
    if (separator < 0) continue;
    const key = line.slice(0, separator).trim();
    const value = line.slice(separator + 1).trim();
    if (key.startsWith("metadata.")) {
      definition.metadata = {...definition.metadata, [key.slice(9)]: value};
      continue;
    }
    switch (key) {
      case "agent_name": definition.agent_name = value; break;
      case "version": definition.version = Number(value.replace(/^v/, "")); break;
      case "target_provider": case "target": definition.target_provider = value as AgentDefinition["target_provider"]; break;
      case "model": definition.model = value; break;
      case "tools_allowed": definition.tools_allowed = list(value); break;
      case "description": definition.description = value; break;
      case "tags": definition.tags = list(value); break;
      case "created_at": definition.created_at = value; break;
    }
  }
  return definition.agent_name ? definition : null;
}

export default function AgentDefinitionViewer(props: ViewerProps) {
  const desktop = useDesktop();
  const [raw, setRaw] = useState(false);
  const source = bodyText(props.document.body);
  const definition = useMemo(() => parseDefinition(source), [source]);

  if (!definition || raw) {
    return (
      <div className="stack">
        {definition && <div className="viewer-tools"><span className="spacer" /><button type="button" className="btn small ghost on" onClick={() => setRaw(false)}>Rendered</button></div>}
        <TextViewer {...props} />
      </div>
    );
  }
  return (
    <div className="entity">
      <header className="entity-head">
        <h2>{definition.agent_name}</h2>
        <Badge tone="purple">{definition.target_provider}</Badge>
        {definition.model && <code className="muted">{definition.model}</code>}
        <span className="spacer" />
        <div className="entity-actions">
          <button type="button" className="btn small ghost" onClick={() => setRaw(true)}>Raw file</button>
          {!props.readOnly && (
            <button type="button" className="btn small" onClick={() => desktop.openDialog("agent", definition)}>New version</button>
          )}
        </div>
      </header>
      {definition.description && <p className="lead">{definition.description}</p>}
      <KeyValue rows={[
        ["Version", `v${definition.version}`],
        ["Created", formatDate(definition.created_at)],
        ["Tools allowed", definition.tools_allowed?.length
          ? <Chips key="tools" items={definition.tools_allowed} tone="purple" />
          : <span key="none" className="muted">none granted (strict allowlist)</span>],
        ["Tags", definition.tags?.length ? <Chips key="tags" items={definition.tags} /> : ""],
        ["Metadata", definition.metadata ? <pre key="meta" className="inline-code">{JSON.stringify(definition.metadata, null, 2)}</pre> : ""],
      ]} />
      <section>
        <h3>Instructions</h3>
        <div className="doc-page"><Markdown source={definition.body} /></div>
      </section>
    </div>
  );
}
