import {useState} from "react";
import {useQuery} from "@tanstack/react-query";
import {api} from "../../api";
import {familyIcon} from "../Icons";
import {Badge, ErrorPanel, Spinner, StatusBadge} from "../ui";
import {formatDate} from "../../lib/format";
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
import {LogViewer, TextViewer} from "./TextViewer";

export type ViewerProps = {
  document: ViewerDocument;
  // readOnly is set while an older version is displayed.
  readOnly: boolean;
};

type ViewerComponent = (props: ViewerProps) => React.ReactNode;

// Semantic families pick their viewer by artifact type; free-form artifacts pick
// by media type, with OpenAPI detected from content.
const byArtifactType: Record<string, ViewerComponent> = {
  requirement: RequirementViewer,
  "agent-definition": AgentDefinitionViewer,
  "override-rule": OverrideRuleViewer,
  "test-suite": TestSuiteViewer,
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

const versionedFamilies = new Set(["requirements", "agent-definitions", "override-rules", "test-suites"]);

// ItemViewer loads one entity, offers its retained versions, and hands the
// document to the matching viewer.
export function ItemViewer({family, ref_}: {family: string; ref_: string}) {
  const {projectRef} = useDesktop();
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
  const readOnly = Boolean(version && current && version !== current);
  const Component = pickViewer(document.data);
  const HeadIcon = familyIcon(family, document.data.artifactType);

  return (
    <div className="item-viewer">
      <header className="item-head">
        <HeadIcon size={16} className={`fam fam-${family} type-${document.data.artifactType}`} />
        <span className="item-family">{familyLabels[family] || family}</span>
        <span className="item-name" title={document.data.name}>{document.data.name}</span>
        <Badge>{document.data.artifactType}</Badge>
        {document.data.mediaType && !["application/json", "text/plain"].includes(document.data.mediaType) && (
          <span className="muted small mono">{document.data.mediaType}</span>
        )}
        <span className="spacer" />
        {history.data && history.data.length > 0 && (
          <VersionSwitcher versions={history.data} selected={version || current || document.data.version || 1} onSelect={(next) => setVersion(next === current ? undefined : next)} />
        )}
      </header>
      {readOnly && (
        <div className="notice">
          Viewing version {version}. The current version is v{current}; older versions are read-only snapshots.
          <button type="button" className="btn small" onClick={() => setVersion(undefined)}>Back to current</button>
        </div>
      )}
      <div className="item-body">
        <Component document={document.data} readOnly={readOnly} key={`${document.data.ref}:${document.data.version || "current"}`} />
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
