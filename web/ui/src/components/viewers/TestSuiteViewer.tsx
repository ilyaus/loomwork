import {useState} from "react";
import type {ViewerProps} from ".";
import {Icon} from "../Icons";
import {Badge, Chips, KeyValue, StatusBadge} from "../ui";
import {formatDate, plural} from "../../lib/format";
import {useDesktop} from "../../lib/desktop";
import type {TestCase, TestSuite} from "../../types";
import {TestCaseCard, methodTone, ruleIdFromRef} from "./TestCaseViewer";

export default function TestSuiteViewer({document}: ViewerProps) {
  return <SuiteDetail suite={document.body as TestSuite} />;
}

// SuiteDetail is shared by the suite tab and the expanded row in the suites list.
export function SuiteDetail({suite, compact = false}: {suite: TestSuite; compact?: boolean}) {
  const desktop = useDesktop();
  const [openCase, setOpenCase] = useState<string | null>(null);
  const cases = suite.cases || [];
  const unlinked = cases.filter((testCase) => !testCase.requirement_ids?.length).length;
  const requirementVersions = Object.entries(suite.requirement_versions || {}).sort(([a], [b]) => a.localeCompare(b));

  return (
    <div className="suite-detail">
      {!compact && (
        <header className="suite-detail-head">
          <h2>{suite.title || suite.suite_id}</h2>
          <StatusBadge status={suite.incomplete ? "incomplete" : "ready"} />
          <Badge tone={suite.origin === "generated" ? "info" : "neutral"}>{suite.origin}</Badge>
        </header>
      )}
      {suite.description && <p className="suite-desc">{suite.description}</p>}
      {suite.incomplete && (
        <div className="warning-panel">
          <Icon.Warning size={15} />
          <div>
            <b>Flagged incomplete.</b> This suite is stored but cannot be trusted as complete:
            <ul>{suite.incomplete_reasons?.map((reason) => <li key={reason}>{reason}</li>)}</ul>
          </div>
        </div>
      )}
      <KeyValue rows={[
        ["Suite id", <code key="id">{suite.suite_id}</code>],
        ["Version", `v${suite.version}`],
        ["Created", formatDate(suite.created_at)],
        ["Agent definition", suite.agent_definition ? (
          <button type="button" className="chip chip-purple chip-link" key="agent" onClick={() => desktop.openItem({family: "agent-definitions", ref: ruleIdFromRef(suite.agent_definition || ""), name: suite.agent_definition || ""})}>
            {suite.agent_definition}
          </button>
        ) : ""],
        ["Spec", suite.spec_ref || ""],
        ["Spec digest", suite.metadata?.spec_sha256 ? <code key="sha" className="small">{suite.metadata.spec_sha256.slice(0, 16)}…</code> : ""],
        ["Imported from", suite.metadata?.imported_from || ""],
        ["Requirement versions", requirementVersions.length ? (
          <Chips key="reqs" items={requirementVersions.map(([id, version]) => `${id} v${version}`)} tone="ok" onClick={(label) => desktop.openItem({family: "requirements", ref: label.split(" ")[0], name: label.split(" ")[0]})} />
        ) : ""],
        ["Override rules", suite.override_rules?.length ? (
          <Chips key="rules" items={suite.override_rules} tone="purple" onClick={(ref) => desktop.openItem({family: "override-rules", ref: ruleIdFromRef(ref), name: ref})} />
        ) : <span key="none" className="muted">none in force</span>],
        ["Tags", suite.tags?.length ? <Chips key="tags" items={suite.tags} /> : ""],
      ]} />

      <section className="cases">
        <h3>{plural(cases.length, "test case")}{unlinked ? <span className="muted"> · {unlinked} unlinked</span> : null}</h3>
        {cases.length === 0 && <p className="muted">This version has no cases.</p>}
        <div className="case-table">
          {cases.map((testCase) => (
            <CaseRow
              testCase={testCase}
              open={openCase === testCase.id}
              onToggle={() => setOpenCase((current) => current === testCase.id ? null : testCase.id)}
              onOpenTab={() => desktop.openItem({family: "test-cases", ref: `${suite.suite_id}~${testCase.id}`, name: testCase.name, version: suite.version, artifactType: "test-case"})}
              key={testCase.id}
            />
          ))}
        </div>
      </section>
    </div>
  );
}

function CaseRow({testCase, open, onToggle, onOpenTab}: {testCase: TestCase; open: boolean; onToggle: () => void; onOpenTab: () => void}) {
  const linked = testCase.requirement_ids?.length > 0;
  return (
    <div className={`case-row ${open ? "open" : ""} ${linked ? "" : "unlinked"}`}>
      <button type="button" className="case-summary" onClick={onToggle} aria-expanded={open}>
        <Icon.Chevron size={13} className={open ? "rot90" : ""} />
        <code className="case-id">{testCase.id}</code>
        <span className="case-name">{testCase.name}</span>
        <Badge tone={methodTone[testCase.request?.method] || "neutral"}>{testCase.request?.method}</Badge>
        <code className="path">{testCase.request?.path}</code>
        <b className={`status-code s${String(testCase.expected?.status)[0]}xx`}>{testCase.expected?.status}</b>
        <span className="case-links">
          {linked
            ? testCase.requirement_ids.map((id) => <span className="chip chip-ok" key={id}>{id}</span>)
            : <span className="chip chip-warn" title="No requirement link">unlinked</span>}
          {testCase.overrides_applied?.map((ref) => <span className="chip chip-purple" key={ref}>{ref}</span>)}
        </span>
      </button>
      {open && (
        <div className="case-expanded">
          <TestCaseCard testCase={testCase} compact />
          <div className="form-actions left"><button type="button" className="btn small ghost" onClick={onOpenTab}>Open in a tab</button></div>
        </div>
      )}
    </div>
  );
}
