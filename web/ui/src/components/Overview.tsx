import {useMemo} from "react";
import {useQuery} from "@tanstack/react-query";
import {api} from "../api";
import {Icon} from "./Icons";
import {Badge, Chips, EmptyState, ErrorPanel, SafeLink, StatusBadge} from "./ui";
import {formatDate, percent, plural, timeAgo} from "../lib/format";
import {useDesktop} from "../lib/desktop";
import type {Project, Requirement, TreeGroup} from "../types";

export default function Overview({project, groups}: {project: Project; groups: TreeGroup[]}) {
  const desktop = useDesktop();
  const testability = useQuery({queryKey: ["testability", project.id], queryFn: () => api.testability(project.id)});
  const requirements = useQuery({queryKey: ["requirements", project.id], queryFn: () => api.listRequirements(project.id)});
  const byFamily = useMemo(() => new Map(groups.map((group) => [group.family, group.items])), [groups]);
  const requirementText = useMemo(
    () => new Map((requirements.data || []).map((requirement: Requirement) => [requirement.id, requirement])),
    [requirements.data],
  );
  const health = testability.data;
  const latestArtifacts = useMemo(() => {
    const newest = new Map<string, Project["artifacts"][number]>();
    for (const artifact of project.artifacts) {
      const current = newest.get(artifact.name);
      if (!current || artifact.version > current.version) newest.set(artifact.name, artifact);
    }
    const visible = new Set(byFamily.get("artifacts")?.map(item => item.ref));
    return [...newest.values()].filter(artifact => visible.has(artifact.id)).sort((a, b) => b.createdAt.localeCompare(a.createdAt)).slice(0, 6);
  }, [project.artifacts, byFamily]);

  return (
    <div className="overview">
      <header className="overview-head">
        <div className="overview-title">
          <h1>{project.name}</h1>
          <p className="muted">{project.description || "No description yet."}</p>
          <div className="overview-meta">
            <Chips items={project.tags} />
            <span className="muted small">created {formatDate(project.createdAt)} · updated {timeAgo(project.updatedAt)}</span>
            <code className="muted small" title="Project id">{project.id}</code>
          </div>
        </div>
        <div className="quick-actions">
          {!desktop.requirementsReadOnly && <button type="button" className="btn primary" onClick={() => desktop.openDialog("requirement")}><Icon.Plus size={14} /> Requirement</button>}
          <button type="button" className="btn" onClick={() => desktop.openDialog("artifact")}><Icon.Artifact size={14} /> Add artifact</button>
          <button type="button" className="btn" onClick={() => desktop.openDialog("suite")}><Icon.Upload size={14} /> Import suite</button>
          <button type="button" className="btn" onClick={() => desktop.openDialog("report")}><Icon.Report size={14} /> Add report</button>
          <button type="button" className="btn" onClick={() => desktop.openDialog("agent")}><Icon.Agent size={14} /> Agent definition</button>
          <button type="button" className="btn" onClick={() => desktop.openDialog("rule")}><Icon.Rule size={14} /> Override rule</button>
        </div>
      </header>

      {project.import && <div className="import-notice">
        <Badge tone="info">{project.import.format} snapshot</Badge>
        <span>Imported {formatDate(project.import.importedAt)} · {project.import.features.length} features</span>
        <code>{project.import.sourcePath}</code>
        <span>Requirements are read-only. QA folders are grouped under Test Suites; folder settings and test edits affect only this local copy.</span>
        <button type="button" className="btn small" onClick={() => desktop.openFamily("artifacts")}>Browse imported documents</button>
      </div>}
      {testability.error && <ErrorPanel error={testability.error} />}

      <section className="stat-grid">
        <button type="button" className="stat" onClick={() => desktop.openFamily("requirements")}>
          <span className="stat-label"><Icon.Requirement size={14} /> Requirements</span>
          <span className="stat-value">{project.index?.activeRequirements ?? health?.activeRequirements ?? 0}</span>
          <span className="stat-sub">{project.index?.requirements ?? 0} total incl. obsolete</span>
        </button>
        <div className={`stat ${health?.coveragePercent === null || !health ? "" : health.coveragePercent >= 80 ? "good" : health.coveragePercent >= 40 ? "warn" : "bad"}`}>
          <span className="stat-label"><Icon.Check size={14} /> Coverage</span>
          <span className="stat-value">{health?.available && health.coveragePercent !== null ? percent(health.coveragePercent) : "–"}</span>
          <span className="stat-sub">
            {health?.available && health.coveragePercent !== null
              ? `${health.coveredRequirements.length} covered · ${health.uncoveredRequirements.length} gaps`
              : "no native test coverage yet"}
          </span>
        </div>
        <button type="button" className="stat" onClick={() => desktop.openFamily("test-suites")}>
          <span className="stat-label"><Icon.Suite size={14} /> Test suites</span>
          <span className="stat-value">{byFamily.get("test-suites")?.length ?? health?.suites ?? 0}</span>
          <span className="stat-sub">
            {plural(health?.cases ?? 0, "native case")} · {plural(byFamily.get("test-suites")?.filter(item => item.artifactType === "document-suite").length ?? 0, "document suite")}
            {health?.incompleteSuites ? ` · ${health.incompleteSuites} incomplete` : ""}
            {health?.unlinkedCases ? ` · ${health.unlinkedCases} unlinked` : ""}
          </span>
        </button>
        <div className={`stat ${health?.lastRun ? (health.lastRun.failed > 0 ? "bad" : "good") : ""}`}>
          <span className="stat-label"><Icon.Report size={14} /> Last run</span>
          <span className="stat-value">
            {health?.lastRun ? <>{health.lastRun.passed}<small className="muted"> / {health.lastRun.total} passed</small></> : "–"}
          </span>
          <span className="stat-sub">
            {health?.lastTestedAt ? `${timeAgo(health.lastTestedAt)} · ${plural(health.reports, "report")}` : "no reports yet"}
          </span>
        </div>
      </section>

      <div className="overview-grid">
        <section className="panel">
          <header className="panel-head">
            <h2><Icon.Warning size={15} /> Open gaps</h2>
            <span className="muted">{health?.uncoveredRequirements.length ?? 0} active requirements without a test case</span>
          </header>
          {health?.uncoveredRequirements.length ? (
            <ul className="gap-list">
              {health.uncoveredRequirements.map((id) => (
                <li key={id}>
                  <button type="button" className="link-row" onClick={() => desktop.openItem({family: "requirements", ref: id, name: id})}>
                    <code>{desktop.requirementName(id)}</code>
                    <span className="row-text">{requirementText.get(id)?.text || ""}</span>
                  </button>
                </li>
              ))}
            </ul>
          ) : health?.available ? (
            <p className="panel-empty ok"><Icon.Check size={15} /> Every active requirement has at least one linked test case.</p>
          ) : (
            <p className="panel-empty">Import or generate a test suite to see which requirements it covers.</p>
          )}
          {health && health.coveredRequirements.length > 0 && (
            <details className="covered">
              <summary>{plural(health.coveredRequirements.length, "covered requirement")}</summary>
              <Chips items={health.coveredRequirements.map(desktop.requirementName)} tone="ok" onClick={(id) => desktop.openItem({family: "requirements", ref: id, name: id})} />
            </details>
          )}
        </section>

        <section className="panel">
          <header className="panel-head">
            <h2><Icon.Link size={15} /> Documentation sources</h2>
            <button type="button" className="btn small" onClick={() => desktop.openDialog("source")}><Icon.Plus size={13} /> Link source</button>
          </header>
          {project.sources?.length ? (
            <table className="table">
              <thead><tr><th>Name</th><th>Type</th><th>Location</th></tr></thead>
              <tbody>
                {project.sources.map((source) => (
                  <tr key={source.name}>
                    <td><b>{source.name}</b></td>
                    <td><Badge>{source.type}</Badge></td>
                    <td className="truncate"><SafeLink value={source.url || source.localPath || source.s3Uri || ""} /></td>
                  </tr>
                ))}
              </tbody>
            </table>
          ) : <p className="panel-empty">No sources linked. Requirements cite their source document by type and reference.</p>}
        </section>

        <section className="panel">
          <header className="panel-head">
            <h2><Icon.Agent size={15} /> Agents and rules</h2>
            <span className="muted">
              {plural(byFamily.get("agent-definitions")?.length ?? 0, "definition")} · {plural(byFamily.get("override-rules")?.length ?? 0, "rule")}
            </span>
          </header>
          <ul className="row-list">
            {byFamily.get("agent-definitions")?.map((item) => (
              <li key={item.ref}>
                <button type="button" className="link-row" onClick={() => desktop.openItem(item)}>
                  <Icon.Agent size={14} className="fam fam-agent-definitions" />
                  <span className="row-text">{item.name}</span>
                  <small className="mono muted">v{item.version}</small>
                </button>
              </li>
            ))}
            {byFamily.get("override-rules")?.map((item) => (
              <li key={item.ref}>
                <button type="button" className="link-row" onClick={() => desktop.openItem(item)}>
                  <Icon.Rule size={14} className="fam fam-override-rules" />
                  <span className="row-text">{item.name}</span>
                  <StatusBadge status={item.status} />
                  <small className="mono muted">v{item.version}</small>
                </button>
              </li>
            ))}
          </ul>
          {!byFamily.get("agent-definitions")?.length && !byFamily.get("override-rules")?.length && (
            <p className="panel-empty">No agent definitions or override rules yet. Override rules tell the generator where business rules beat the literal spec.</p>
          )}
        </section>

        <section className="panel">
          <header className="panel-head">
            <h2><Icon.Artifact size={15} /> Recent artifacts</h2>
            <span className="muted">{plural(project.artifacts.length, "revision")}</span>
          </header>
          {latestArtifacts.length ? (
            <ul className="row-list">
              {latestArtifacts.map((artifact) => (
                <li key={artifact.id}>
                  <button type="button" className="link-row" onClick={() => desktop.openItem({family: "artifacts", ref: artifact.id, name: artifact.name, version: artifact.version, artifactType: artifact.type})}>
                    <Icon.Artifact size={14} className={`fam type-${artifact.type}`} />
                    <span className="row-text">{artifact.name}</span>
                    <Badge>{artifact.type}</Badge>
                    {artifact.pinned && <Icon.Pin size={13} className="muted" />}
                    <small className="mono muted">v{artifact.version}</small>
                    <small className="muted">{timeAgo(artifact.createdAt)}</small>
                  </button>
                </li>
              ))}
            </ul>
          ) : <EmptyState title="No artifacts yet">Specs, logs, and docs the agent can reason over.</EmptyState>}
        </section>
      </div>
    </div>
  );
}
