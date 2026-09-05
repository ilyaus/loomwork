import {useMutation} from "@tanstack/react-query";
import type {ViewerProps} from ".";
import {api} from "../../api";
import {Badge, Chips, ErrorPanel, KeyValue, StatusBadge} from "../ui";
import {formatDate} from "../../lib/format";
import {useDesktop} from "../../lib/desktop";
import type {OverrideRule} from "../../types";
import {methodTone} from "./TestCaseViewer";

export const actionLabels: Record<string, string> = {
  "expect-status": "Expect a specific status",
  "expect-empty-collection": "Expect an empty collection",
  "skip-test": "Do not test this case",
};

export function describeAction(action: OverrideRule["action"]): string {
  switch (action.kind) {
    case "expect-empty-collection": return `Expect ${action.expect_status || 200} with an empty collection body`;
    case "skip-test": return "Do not generate a test for the matched condition";
    default: return `Expect HTTP ${action.expect_status}`;
  }
}

export default function OverrideRuleViewer({document, readOnly}: ViewerProps) {
  const desktop = useDesktop();
  const rule = document.body as OverrideRule;
  const status = useMutation({
    mutationFn: (next: "active" | "obsolete") => api.setOverrideRuleStatus(desktop.projectRef, rule.id, next),
    onSuccess: desktop.refresh,
  });
  const condition = rule.condition || {};
  const conditionEmpty = !condition.methods?.length && !condition.path_pattern && !condition.scenario && !condition.spec_status;
  const isActive = rule.status === "active";

  return (
    <div className="entity">
      <header className="entity-head">
        <h2>{rule.title}</h2>
        <StatusBadge status={rule.status} />
        <span className="spacer" />
        {!readOnly && (
          <div className="entity-actions">
            <button type="button" className="btn small" onClick={() => desktop.openDialog("rule", rule)}>New version</button>
            {rule.status !== "superseded" && (
              <button type="button" className="btn small" onClick={() => status.mutate(isActive ? "obsolete" : "active")} disabled={status.isPending}>
                {isActive ? "Mark obsolete" : "Reactivate"}
              </button>
            )}
          </div>
        )}
      </header>
      {status.error && <ErrorPanel error={status.error} />}

      <div className="rule-grid">
        <section className="rule-box">
          <h3>When a test case matches</h3>
          {conditionEmpty ? <p className="muted">Any case. An empty condition governs every generated test.</p> : (
            <KeyValue rows={[
              ["Methods", condition.methods?.length ? (
                <span key="m" className="chips">{condition.methods.map((method) => <Badge tone={methodTone[method] || "neutral"} key={method}>{method}</Badge>)}</span>
              ) : ""],
              ["Path", condition.path_pattern ? <code key="p">{condition.path_pattern}</code> : ""],
              ["Scenario", condition.scenario ? <Badge key="s">{condition.scenario}</Badge> : ""],
              ["Spec says", condition.spec_status ? <span key="ss">HTTP {condition.spec_status} <span className="muted">(the literal reading this rule corrects)</span></span> : ""],
            ]} />
          )}
        </section>
        <section className="rule-box action">
          <h3>Then the expected behavior is</h3>
          <p className="rule-action"><b>{actionLabels[rule.action?.kind] || rule.action?.kind}</b></p>
          <p className="muted">{describeAction(rule.action)}</p>
        </section>
      </div>

      <section>
        <h3>Rationale</h3>
        <blockquote className="req-quote">{rule.rationale}</blockquote>
        <p className="muted small">The agent reasons over this text to generalize the rule beyond the structured condition; the workbench audits the condition mechanically.</p>
      </section>

      <KeyValue rows={[
        ["Rule id", <code key="id">{rule.id}</code>],
        ["Citation", <code key="ref">{rule.id}-v{rule.version}</code>],
        ["Created", formatDate(rule.created_at)],
        ["Tags", rule.tags?.length ? <Chips key="tags" items={rule.tags} /> : ""],
      ]} />
    </div>
  );
}
