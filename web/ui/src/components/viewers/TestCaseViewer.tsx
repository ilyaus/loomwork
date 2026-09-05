import type {ViewerProps} from ".";
import {Badge, Chips, KeyValue} from "../ui";
import {useDesktop} from "../../lib/desktop";
import type {TestCase} from "../../types";

export const methodTone: Record<string, string> = {
  GET: "info", POST: "ok", PUT: "warn", PATCH: "warn", DELETE: "danger", HEAD: "neutral", OPTIONS: "neutral",
};

// ruleIdFromRef strips the -vN suffix from an override citation.
export function ruleIdFromRef(ref: string): string {
  return ref.replace(/-v\d+$/, "");
}

export default function TestCaseViewer({document}: ViewerProps) {
  return <TestCaseCard testCase={document.body as TestCase} />;
}

// TestCaseCard renders one case; requirement and override chips open the linked
// entity so traceability is one click away.
export function TestCaseCard({testCase, compact = false}: {testCase: TestCase; compact?: boolean}) {
  const desktop = useDesktop();
  const request = testCase.request || {method: "GET", path: ""};
  const expected = testCase.expected || {status: 0};
  const hasRequestDetail = request.query || request.headers || request.body;
  return (
    <div className={`case-card ${compact ? "compact" : ""}`}>
      {!compact && (
        <header className="case-head">
          <code className="case-id">{testCase.id}</code>
          <h2>{testCase.name}</h2>
          <Badge tone="neutral">{testCase.scenario}</Badge>
        </header>
      )}
      <div className="request-line">
        <Badge tone={methodTone[request.method] || "neutral"}>{request.method}</Badge>
        <code className="path">{request.path}</code>
        <span className="arrow" aria-hidden="true">→</span>
        <b className={`status-code s${String(expected.status)[0]}xx`}>{expected.status}</b>
        {expected.empty_collection && <Badge tone="info">empty collection</Badge>}
        {expected.max_latency_ms ? <Badge>≤ {expected.max_latency_ms} ms</Badge> : null}
      </div>
      <div className="trace-grid">
        <section>
          <h3>Requirements</h3>
          <Chips
            items={testCase.requirement_ids}
            tone="ok"
            empty="No linked requirement (flags the suite incomplete)"
            onClick={(id) => desktop.openItem({family: "requirements", ref: id, name: id})}
          />
        </section>
        <section>
          <h3>Overrides applied</h3>
          <Chips
            items={testCase.overrides_applied}
            tone="purple"
            empty="None"
            onClick={(ref) => desktop.openItem({family: "override-rules", ref: ruleIdFromRef(ref), name: ref})}
          />
        </section>
      </div>
      {(hasRequestDetail || expected.body_fields?.length || expected.notes) && (
        <div className="trace-grid">
          {hasRequestDetail && (
            <section>
              <h3>Request</h3>
              <KeyValue rows={[
                ["Query", request.query ? <pre className="inline-code">{JSON.stringify(request.query, null, 2)}</pre> : ""],
                ["Headers", request.headers ? <pre className="inline-code">{JSON.stringify(request.headers, null, 2)}</pre> : ""],
                ["Body", request.body ? <pre className="inline-code">{request.body}</pre> : ""],
                ["Body type", request.body_media_type || ""],
              ]} />
            </section>
          )}
          {(expected.body_fields?.length || expected.notes) && (
            <section>
              <h3>Expected</h3>
              <KeyValue rows={[
                ["Body fields", expected.body_fields?.length ? <Chips items={expected.body_fields} /> : ""],
                ["Notes", expected.notes || ""],
              ]} />
            </section>
          )}
        </div>
      )}
      {testCase.tags?.length ? <div className="case-tags"><Chips items={testCase.tags} /></div> : null}
    </div>
  );
}
