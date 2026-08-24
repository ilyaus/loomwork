import {FormEvent, lazy, Suspense, useMemo, useState} from "react";
import {useMutation, useQuery, useQueryClient} from "@tanstack/react-query";
import {api} from "../api";
import type {Requirement, ViewerDocument} from "../types";

const SwaggerUI = lazy(() => import("swagger-ui-react"));

export type ViewerProps = {
  document: ViewerDocument;
  projectRef: string;
  refresh: () => void;
};

export type ViewerComponent = (props: ViewerProps) => React.ReactNode;

function bodyText(body: unknown): string {
  return typeof body === "string" ? body : JSON.stringify(body, null, 2);
}

function TextViewer({document}: ViewerProps) {
  return <pre className="code">{bodyText(document.body)}</pre>;
}

function HtmlViewer({document}: ViewerProps) {
  if (typeof document.body !== "string") return <TextViewer document={document} projectRef="" refresh={() => undefined} />;
  return <iframe className="html-viewer" srcDoc={document.body} sandbox="" title={document.name} />;
}

function OpenAPIViewer({document}: ViewerProps) {
  const spec = useMemo(() => {
    if (typeof document.body !== "string") return document.body;
    try {
      return JSON.parse(document.body) as object;
    } catch {
      return null;
    }
  }, [document.body]);
  if (!spec) return <TextViewer document={document} projectRef="" refresh={() => undefined} />;
  return (
    <div className="swagger-viewer">
      <Suspense fallback={<div className="empty-state">Loading OpenAPI viewer…</div>}>
        <SwaggerUI spec={spec} supportedSubmitMethods={[]} />
      </Suspense>
    </div>
  );
}

type TestCase = {
  id?: string;
  name?: string;
  requirement_ids?: string[];
  overrides_applied?: string[];
  scenario?: string;
  request?: {
    method?: string;
    path?: string;
    query?: Record<string, string>;
    headers?: Record<string, string>;
    body?: string;
    body_media_type?: string;
  };
  expected?: {
    status?: number;
    empty_collection?: boolean;
    body_fields?: string[];
    max_latency_ms?: number;
    notes?: string;
  };
};

function TestCaseViewer({document}: ViewerProps) {
  const testCase = document.body as TestCase;
  return (
    <div className="structured-viewer">
      <header>
        <span className="tag tg-test">{testCase.scenario || "test case"}</span>
        <h2>{testCase.name || testCase.id || document.name}</h2>
        <span className="mono dim">{testCase.id}</span>
      </header>
      <div className="trace-grid">
        <section>
          <h3>Requirement links</h3>
          <div className="chips">
            {testCase.requirement_ids?.map((id) => <span className="chip linked" key={id}>{id}</span>)}
            {!testCase.requirement_ids?.length && <span className="dim">No linked requirements</span>}
          </div>
        </section>
        <section>
          <h3>Applied overrides</h3>
          <div className="chips">
            {testCase.overrides_applied?.map((ref) => <span className="chip override" key={ref}>{ref}</span>)}
            {!testCase.overrides_applied?.length && <span className="dim">No applied overrides</span>}
          </div>
        </section>
      </div>
      <section className="request-card">
        <h3>Request</h3>
        <div className="request-line">
          <b>{testCase.request?.method}</b>
          <code>{testCase.request?.path}</code>
        </div>
        {(testCase.request?.query || testCase.request?.headers || testCase.request?.body) && (
          <pre className="code compact">{JSON.stringify(testCase.request, null, 2)}</pre>
        )}
      </section>
      <section className="expect-card">
        <h3>Expected outcome</h3>
        <div className="expect-status">{testCase.expected?.status}</div>
        {testCase.expected?.empty_collection && <span className="chip">empty collection</span>}
        {testCase.expected?.max_latency_ms && <span className="chip">≤ {testCase.expected.max_latency_ms} ms</span>}
        {testCase.expected?.body_fields?.length ? (
          <p>Body fields: <span className="mono">{testCase.expected.body_fields.join(", ")}</span></p>
        ) : null}
        {testCase.expected?.notes && <p>{testCase.expected.notes}</p>}
      </section>
    </div>
  );
}

type ReportRow = {
  id?: string;
  name?: string;
  title?: string;
  file?: string;
  scenario?: number;
  status?: string;
  passed?: boolean;
  latency_ms?: number;
  duration_ms?: number;
  durationMs?: number;
  reason?: string;
  error?: string;
  skipReason?: string;
  transportError?: string;
  tokenError?: string;
};

function TestReportViewer({document}: ViewerProps) {
  let report: Record<string, unknown> | null = null;
  try {
    report = typeof document.body === "string"
      ? JSON.parse(document.body) as Record<string, unknown>
      : document.body as Record<string, unknown>;
  } catch {
    return <TextViewer document={document} projectRef="" refresh={() => undefined} />;
  }
  const rows = [
    ...(Array.isArray(report.tests) ? report.tests : []),
    ...(Array.isArray(report.results) ? report.results : []),
    ...(Array.isArray(report.failures) ? report.failures : []),
  ] as ReportRow[];
  const uniqueRows = rows.filter((row, index) =>
    rows.findIndex((candidate) =>
      `${candidate.id || candidate.file || candidate.name}:${candidate.scenario || ""}` ===
      `${row.id || row.file || row.name}:${row.scenario || ""}`,
    ) === index,
  );
  const summary = report.summary && typeof report.summary === "object"
    ? report.summary as Record<string, unknown>
    : {};
  const passed = typeof summary.passed === "number"
    ? summary.passed
    : typeof report.passed === "number"
      ? report.passed
      : uniqueRows.filter((row) => row.passed || row.status === "passed").length;
  const failed = typeof summary.failed === "number"
    ? summary.failed
    : typeof report.failed === "number"
      ? report.failed
      : uniqueRows.filter((row) => row.passed === false || row.status === "failed" || row.error).length;
  const skipped = typeof summary.skipped === "number"
    ? summary.skipped
    : uniqueRows.filter((row) => row.status === "skipped").length;
  const total = typeof summary.total === "number"
    ? summary.total
    : typeof report.total === "number"
      ? report.total
      : uniqueRows.length || Number(passed) + Number(failed) + Number(skipped);
  return (
    <div className="structured-viewer report-viewer">
      <header>
        <span className="tag tg-test">test report</span>
        <h2>{document.name}</h2>
      </header>
      <div className="report-counts">
        <div><b className="pass">{String(passed)}</b><span>passed</span></div>
        <div><b className="fail">{String(failed)}</b><span>failed</span></div>
        {Number(skipped) > 0 && <div><b className="skip">{String(skipped)}</b><span>skipped</span></div>}
        <div><b>{String(total)}</b><span>total</span></div>
      </div>
      {uniqueRows.length > 0 ? (
        <div className="report-table">
          {uniqueRows.map((row, index) => {
            const status = row.status || (row.passed === false || row.error ? "failed" : "passed");
            const label = row.title || row.name || row.id || row.file || `test ${index + 1}`;
            const detail = row.reason || row.error || row.skipReason || row.transportError || row.tokenError;
            const latency = row.latency_ms ?? row.duration_ms ?? row.durationMs;
            return (
              <div className={`report-row ${status}`} key={`${row.id || row.file || row.name || index}:${row.scenario || ""}`}>
                <span className="status-dot" />
                <b>{label}</b>
                <span>{detail || (row.scenario ? `scenario ${row.scenario}` : status)}</span>
                <code>{latency === undefined ? "—" : `${latency} ms`}</code>
              </div>
            );
          })}
        </div>
      ) : <pre className="code compact">{JSON.stringify(report, null, 2)}</pre>}
    </div>
  );
}

function TestSuiteViewer({document}: ViewerProps) {
  const suite = document.body as {
    suite_id?: string; title?: string; description?: string; version?: number;
    origin?: string; incomplete?: boolean; incomplete_reasons?: string[];
    cases?: TestCase[]; requirement_versions?: Record<string, number>; override_rules?: string[];
  };
  return (
    <div className="structured-viewer">
      <header>
        <span className="tag tg-test">{suite.origin || "test suite"}</span>
        <h2>{suite.title || suite.suite_id || document.name}</h2>
        <span className="mono dim">v{suite.version}</span>
      </header>
      {suite.description && <p>{suite.description}</p>}
      {suite.incomplete && (
        <div className="warning-panel">
          Incomplete suite: {suite.incomplete_reasons?.join("; ") || "traceability gaps are present"}
        </div>
      )}
      <div className="trace-grid">
        <section>
          <h3>Requirement versions</h3>
          <div className="chips">
            {Object.entries(suite.requirement_versions || {}).map(([id, version]) => (
              <span className="chip linked" key={id}>{id}-v{version}</span>
            ))}
          </div>
        </section>
        <section>
          <h3>Override rules</h3>
          <div className="chips">
            {suite.override_rules?.map((rule) => <span className="chip override" key={rule}>{rule}</span>)}
          </div>
        </section>
      </div>
      <section>
        <h3>{suite.cases?.length || 0} test cases</h3>
        <div className="suite-cases">
          {suite.cases?.map((testCase) => (
            <div key={testCase.id}>
              <b>{testCase.id}</b>
              <span>{testCase.name}</span>
              <code>{testCase.request?.method} {testCase.request?.path}</code>
              <span>{testCase.expected?.status}</span>
            </div>
          ))}
        </div>
      </section>
    </div>
  );
}

function RequirementViewer({document, projectRef, refresh}: ViewerProps) {
  const queryClient = useQueryClient();
  const requirement = document.body as Requirement;
  const [text, setText] = useState(requirement.text);
  const [tags, setTags] = useState((requirement.tags || []).join(", "));
  const [editing, setEditing] = useState(false);
  const history = useQuery({
    queryKey: ["requirement-history", projectRef, requirement.id],
    queryFn: () => api.requirementHistory(projectRef, requirement.id),
  });
  const update = useMutation({
    mutationFn: () => api.updateRequirement(projectRef, requirement.id, {
      text,
      tags: tags.split(",").map((tag) => tag.trim()).filter(Boolean),
    }),
    onSuccess: async (updated) => {
      queryClient.setQueryData(
        ["item", projectRef, document.family, document.ref],
        {...document, version: updated.version, body: updated},
      );
      setEditing(false);
      await queryClient.invalidateQueries({queryKey: ["requirements", projectRef]});
      await queryClient.invalidateQueries({queryKey: ["requirement-history", projectRef, requirement.id]});
      refresh();
    },
  });
  const status = useMutation({
    mutationFn: (next: "active" | "obsolete") => api.setRequirementStatus(projectRef, requirement.id, next),
    onSuccess: async (updated) => {
      queryClient.setQueryData(
        ["item", projectRef, document.family, document.ref],
        {...document, version: updated.version, body: updated},
      );
      await queryClient.invalidateQueries({queryKey: ["requirements", projectRef]});
      await queryClient.invalidateQueries({queryKey: ["requirement-history", projectRef, requirement.id]});
      refresh();
    },
  });
  function submit(event: FormEvent) {
    event.preventDefault();
    update.mutate();
  }
  return (
    <div className="structured-viewer requirement-viewer">
      <header>
        <span className={`tag ${requirement.status === "active" ? "tg-doc" : "tg-log"}`}>{requirement.status}</span>
        <h2>{requirement.id}</h2>
        <span className="mono dim">v{requirement.version}</span>
        <button className="btn" onClick={() => setEditing((value) => !value)}>{editing ? "Cancel" : "New version"}</button>
        <button
          className="btn"
          onClick={() => status.mutate(requirement.status === "active" ? "obsolete" : "active")}
        >
          Mark {requirement.status === "active" ? "obsolete" : "active"}
        </button>
      </header>
      {editing ? (
        <form className="requirement-form" onSubmit={submit}>
          <textarea value={text} onChange={(event) => setText(event.target.value)} rows={10} required />
          <input value={tags} onChange={(event) => setTags(event.target.value)} placeholder="tags, comma-separated" />
          <button className="btn primary" disabled={update.isPending}>Save new version</button>
        </form>
      ) : <pre className="code requirement-text">{requirement.text}</pre>}
      {requirement.source_ref && (
        <p className="source-ref">Source: <SafeLink value={requirement.source_ref} /></p>
      )}
      <section>
        <h3>Version history</h3>
        <div className="history-strip">
          {history.data?.map((version) => (
            <span className={`chip ${version.status}`} key={version.version}>
              v{version.version} · {version.status}
            </span>
          ))}
        </div>
      </section>
    </div>
  );
}

export function SafeLink({value}: {value: string}) {
  try {
    const parsed = new URL(value);
    if (parsed.protocol === "http:" || parsed.protocol === "https:") {
      return <a href={parsed.href} target="_blank" rel="noreferrer">{value}</a>;
    }
  } catch {
    // Render malformed and non-http(s) references as inert text.
  }
  return <span className="mono dim">{value}</span>;
}

export const viewerRegistry: Record<string, ViewerComponent> = {
  "text/html": HtmlViewer,
  "application/vnd.oai.openapi+json": OpenAPIViewer,
  "application/vnd.oai.openapi": OpenAPIViewer,
  "test-case": TestCaseViewer,
  "application/vnd.loomwork.test-case+json": TestCaseViewer,
  "test-report": TestReportViewer,
  "test-result": TestReportViewer,
  "test-suite": TestSuiteViewer,
  "requirement": RequirementViewer,
  "text/markdown": TextViewer,
  "text/plain": TextViewer,
  "application/json": TextViewer,
  "application/yaml": TextViewer,
  "spec": TextViewer,
  "log": TextViewer,
  "doc": TextViewer,
  "generated": TextViewer,
};

function looksLikeOpenAPI(body: unknown): boolean {
  if (body && typeof body === "object") {
    return "openapi" in body || "swagger" in body;
  }
  if (typeof body !== "string") return false;
  try {
    const parsed = JSON.parse(body) as Record<string, unknown>;
    return "openapi" in parsed || "swagger" in parsed;
  } catch {
    return false;
  }
}

export function Viewer(props: ViewerProps) {
  const semanticTypes = ["requirement", "test-case", "test-suite", "test-report", "test-result"];
  const key = looksLikeOpenAPI(props.document.body)
    ? "application/vnd.oai.openapi+json"
    : semanticTypes.includes(props.document.artifactType)
      ? props.document.artifactType
      : props.document.mediaType;
  const Component = viewerRegistry[key] || viewerRegistry[props.document.artifactType] || TextViewer;
  return <Component {...props} />;
}
