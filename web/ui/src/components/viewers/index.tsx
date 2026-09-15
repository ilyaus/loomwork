import {useState} from "react";
import {useQuery} from "@tanstack/react-query";
import {api} from "../../api";
import {familyIcon} from "../Icons";
import {Badge, ErrorPanel, Spinner, StatusBadge} from "../ui";
import {baseName, formatDate} from "../../lib/format";
import {familyLabels, useDesktop} from "../../lib/desktop";
import type {ItemVersion, ViewerDocument} from "../../types";
import AgentDefinitionViewer from "./AgentDefinitionViewer";
import HtmlViewer from "./HtmlViewer";
import MarkdownViewer from "./MarkdownViewer";
import OpenAPIViewer, {looksLikeOpenAPI} from "./OpenAPIViewer";
import OverrideRuleViewer from "./OverrideRuleViewer";
import ReportViewer from "./ReportViewer";
import RequirementViewer from "./RequirementViewer";
import TestCaseViewer from "./TestCaseViewer";
import TestSuiteViewer from "./TestSuiteViewer";
import DocumentSuiteViewer from "./DocumentSuiteViewer";
import {LogViewer, TextViewer} from "./TextViewer";

export type ViewerProps = {
  document: ViewerDocument;
  // readOnly is set while an older version is displayed.
  readOnly: boolean;
  selectedTest?: string;
};

type ViewerComponent = (props: ViewerProps) => React.ReactNode;

// Semantic families pick their viewer by artifact type; free-form artifacts pick
// by media type, with OpenAPI detected from content.
const byArtifactType: Record<string, ViewerComponent> = {
  requirement: RequirementViewer,
  "agent-definition": AgentDefinitionViewer,
  "override-rule": OverrideRuleViewer,
  "test-suite": TestSuiteViewer,
  "document-suite": DocumentSuiteViewer,
  "test-case": TestCaseViewer,
  "test-report": ReportViewer,
  "test-result": ReportViewer,
  log: LogViewer,
};

const byMediaType: Record<string, ViewerComponent> = {
  "text/html": HtmlViewer,
  "text/markdown": MarkdownViewer,
  "application/vnd.oai.openapi+json": OpenAPIViewer,
  "application/vnd.oai.openapi": OpenAPIViewer,
};

export function pickViewer(document: ViewerDocument): ViewerComponent {
  if (byArtifactType[document.artifactType]) return byArtifactType[document.artifactType];
  if (looksLikeOpenAPI(document.body)) return OpenAPIViewer;
  const mediaType = document.mediaType.split(";")[0].trim();
  return byMediaType[mediaType] || TextViewer;
}

const versionedFamilies = new Set(["requirements", "agent-definitions", "override-rules", "test-suites", "artifacts"]);

// ItemViewer loads one entity, offers its retained versions, and hands the
// document to the matching viewer.
export function ItemViewer({family, ref_, selectedTest, embedded = false}: {family: string; ref_: string; selectedTest?: string; embedded?: boolean}) {
  const desktop = useDesktop();
  const {projectRef} = desktop;
  const [version, setVersion] = useState<number | undefined>(undefined);
  const document = useQuery({
    queryKey: ["item", projectRef, family, ref_, version || "current"],
    queryFn: () => api.projectItem(projectRef, family, ref_, version),
  });
  const history = useQuery({
    queryKey: ["item-history", projectRef, family, ref_],
    queryFn: () => api.itemHistory(projectRef, family, ref_),
    enabled: versionedFamilies.has(family),
  });

  if (document.isLoading) return <div className="page-pad"><Spinner label="Opening…" /></div>;
  if (document.error) return <div className="page-pad"><ErrorPanel error={document.error} /></div>;
  if (!document.data) return null;

  const current = history.data?.[0]?.version;
  const historical = Boolean(document.data.version && current && document.data.version !== current);
  const readOnly = embedded || historical || (family === "requirements" && desktop.requirementsReadOnly);
  const selectVersion = (next: number | undefined) => setVersion(family !== "artifacts" && next === current ? undefined : next);
  const Component = pickViewer(document.data);
  const HeadIcon = familyIcon(family, document.data.artifactType);

  return (
    <div className={`item-viewer ${embedded ? "embedded" : ""}`}>
      <header className="item-head">
        <HeadIcon size={16} className={`fam fam-${family} type-${document.data.artifactType}`} />
        <span className="item-family">{familyLabels[family] || family}</span>
        <span className="item-name" title={document.data.name}>{["artifacts", "reports"].includes(family) ? baseName(document.data.name) : document.data.name}</span>
        <Badge>{document.data.artifactType}</Badge>
        {document.data.mediaType && !["application/json", "text/plain"].includes(document.data.mediaType) && (
          <span className="muted small mono">{document.data.mediaType}</span>
        )}
        <span className="spacer" />
        {family === "artifacts" && !readOnly && <button type="button" className="btn small" onClick={() => desktop.openDialog("artifact", document.data!.ref)}>New version</button>}
        {history.data && history.data.length > 0 && (
          <VersionSwitcher versions={history.data} selected={document.data.version || 1} onSelect={selectVersion} />
        )}
      </header>
      {historical && (
        <div className="notice">
          Viewing version {document.data.version}. The current version is v{current}; older versions are read-only snapshots.
          <button type="button" className="btn small" onClick={() => selectVersion(current)}>Back to current</button>
        </div>
      )}
      {family === "requirements" && desktop.requirementsReadOnly && <div className="notice">Imported requirement. This snapshot is read-only; test work stays in your local copy.</div>}
      <div className="item-body">
        <Component document={document.data} readOnly={readOnly} selectedTest={selectedTest} key={`${document.data.ref}:${document.data.version || "current"}`} />
      </div>
    </div>
  );
}

function VersionSwitcher({versions, selected, onSelect}: {versions: ItemVersion[]; selected: number; onSelect: (version: number) => void}) {
  const chosen = versions.find((entry) => entry.version === selected);
  return (
    <div className="version-switcher">
      <label>
        <span className="muted small">Version</span>
        <select value={selected} onChange={(event) => onSelect(Number(event.target.value))} aria-label="Version">
          {versions.map((entry, index) => (
            <option value={entry.version} key={entry.version}>
              v{entry.version}{index === 0 ? " (current)" : ""}{entry.status ? ` · ${entry.status}` : ""}
            </option>
          ))}
        </select>
      </label>
      {chosen && <StatusBadge status={chosen.status} />}
      {chosen && <span className="muted small" title={formatDate(chosen.createdAt)}>{formatDate(chosen.createdAt)}</span>}
    </div>
  );
}
