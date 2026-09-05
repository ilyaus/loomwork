import {useMemo, useState} from "react";
import type {ViewerProps} from ".";
import {Badge, KeyValue} from "../ui";
import {formatDate} from "../../lib/format";
import {useDesktop} from "../../lib/desktop";
import {TextViewer, bodyText} from "./TextViewer";

type ReportRow = {
  id?: string;
  name?: string;
  title?: string;
  file?: string;
  scenario?: number | string;
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

type Parsed = {
  report: Record<string, unknown>;
  rows: {label: string; status: string; detail: string; latency?: number; id?: string}[];
  passed: number;
  failed: number;
  skipped: number;
  total: number;
};

const number = (value: unknown): number | undefined => typeof value === "number" ? value : undefined;
const text = (value: unknown): string | undefined => typeof value === "string" ? value : undefined;

// parseReport tolerates api-test-runner reports, the vision's report layout, and
// plain per-test arrays.
function parseReport(body: unknown): Parsed | null {
  let report: Record<string, unknown>;
  try {
    report = typeof body === "string" ? JSON.parse(body) as Record<string, unknown> : body as Record<string, unknown>;
  } catch {
    return null;
  }
  if (!report || typeof report !== "object" || Array.isArray(report)) return null;
  const raw = [
    ...(Array.isArray(report.tests) ? report.tests : []),
    ...(Array.isArray(report.results) ? report.results : []),
    ...(Array.isArray(report.failures) ? report.failures : []),
  ] as ReportRow[];
  const seen = new Set<string>();
  const rows = raw.flatMap((row, index) => {
    const key = `${row.id || row.file || row.name || row.title || index}:${row.scenario ?? ""}`;
    if (seen.has(key)) return [];
    seen.add(key);
    const status = row.status || (row.passed === false || row.error ? "failed" : "passed");
    return [{
      id: row.id,
      label: row.title || row.name || row.id || row.file || `test ${index + 1}`,
      status,
      detail: row.reason || row.error || row.skipReason || row.transportError || row.tokenError || (row.scenario !== undefined ? `scenario ${row.scenario}` : ""),
      latency: row.latency_ms ?? row.duration_ms ?? row.durationMs,
    }];
  });
  const summary = (report.summary && typeof report.summary === "object" ? report.summary : {}) as Record<string, unknown>;
  const count = (key: string, predicate: (status: string) => boolean) =>
    number(summary[key]) ?? number(report[key]) ?? rows.filter((row) => predicate(row.status)).length;
  const passed = count("passed", (status) => status === "passed");
  const failed = count("failed", (status) => status === "failed");
  const skipped = count("skipped", (status) => status === "skipped");
  const total = number(summary.total) ?? number(report.total) ?? (rows.length || passed + failed + skipped);
  return {report, rows, passed, failed, skipped, total};
}

export default function ReportViewer(props: ViewerProps) {
  const desktop = useDesktop();
  const [raw, setRaw] = useState(false);
  const parsed = useMemo(() => parseReport(props.document.body), [props.document.body]);
  if (!parsed || raw) {
    return (
      <div className="stack">
        {parsed && <div className="viewer-tools"><span className="spacer" /><button type="button" className="btn small ghost on" onClick={() => setRaw(false)}>Summary</button></div>}
        {!parsed && <div className="notice">This report is not JSON, so it is shown as text.</div>}
        <TextViewer {...props} />
      </div>
    );
  }
  const {report, rows, passed, failed, skipped, total} = parsed;
  const suiteId = text(report.suite_id) || text(report.suiteId);
  const suiteVersion = number(report.suite_version) ?? number(report.suiteVersion);
  const runAt = text(report.run_timestamp) || text(report.runTimestamp) || text(report.timestamp) || text(report.startedAt);
  const outcome = text(report.outcome) || (failed > 0 ? "failed" : "passed");
  const rate = total ? Math.round((passed / total) * 100) : 0;

  return (
    <div className="report">
      <div className="viewer-tools">
        <Badge tone={failed > 0 ? "danger" : "ok"}>{outcome}</Badge>
        {report.dryRun === true && <Badge tone="warn">dry run</Badge>}
        <span className="spacer" />
        <button type="button" className="btn small ghost" onClick={() => setRaw(true)}>Raw JSON</button>
        <button type="button" className="btn small ghost" onClick={() => void navigator.clipboard?.writeText(bodyText(props.document.body))}>Copy</button>
      </div>
      <div className="report-counts">
        <div className="count-card pass"><b>{passed}</b><span>passed</span></div>
        <div className="count-card fail"><b>{failed}</b><span>failed</span></div>
        <div className="count-card skip"><b>{skipped}</b><span>skipped</span></div>
        <div className="count-card"><b>{total}</b><span>total</span></div>
        <div className="pass-bar" title={`${rate}% passed`}>
          <span style={{width: `${rate}%`}} className={failed > 0 ? "partial" : "full"} />
        </div>
      </div>
      <KeyValue rows={[
        ["Suite", suiteId ? (
          <button type="button" className="chip chip-link" key="suite" onClick={() => desktop.openItem({family: "test-suites", ref: suiteId, name: suiteId})}>
            {suiteId}{suiteVersion ? ` v${suiteVersion}` : ""}
          </button>
        ) : ""],
        ["Run at", runAt ? formatDate(runAt) : ""],
        ["Executor", text(report.executor_mode) || text(report.executorMode) || ""],
        ["Environment", text(report.environment) || text(report.baseUrl) || ""],
      ]} />
      {rows.length > 0 ? (
        <table className="table report-table">
          <thead><tr><th></th><th>Test</th><th>Detail</th><th className="num">Latency</th></tr></thead>
          <tbody>
            {rows.map((row, index) => (
              <tr className={`row-${row.status}`} key={`${row.id || row.label}-${index}`}>
                <td><span className={`dot dot-${row.status}`} title={row.status} /></td>
                <td><b>{row.label}</b>{row.id && row.id !== row.label ? <small className="mono muted"> {row.id}</small> : null}</td>
                <td className="muted">{row.detail || row.status}</td>
                <td className="num mono">{row.latency === undefined ? "–" : `${row.latency} ms`}</td>
              </tr>
            ))}
          </tbody>
        </table>
      ) : <div className="notice">The report has no per-test rows; only aggregate counts are available.</div>}
    </div>
  );
}
