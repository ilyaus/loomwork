import {useState} from "react";
import {useQuery} from "@tanstack/react-query";
import {api} from "../api";
import {Icon} from "./Icons";
import {EmptyState, ErrorPanel, Spinner, StatusBadge} from "./ui";
import {SuiteDetail} from "./viewers/TestSuiteViewer";
import {plural} from "../lib/format";
import {useDesktop} from "../lib/desktop";
import type {TestSuite, TreeItem} from "../types";

export default function TestSuitesView({items}: {items: TreeItem[]}) {
  const desktop = useDesktop();
  const [expanded, setExpanded] = useState<Set<string>>(() => new Set(items.length === 1 ? [items[0].ref] : []));
  const cases = items.reduce((count, item) => count + (item.children?.length || 0), 0);
  const incomplete = items.filter((item) => item.status === "incomplete").length;

  function toggle(ref: string) {
    setExpanded((current) => {
      const next = new Set(current);
      if (next.has(ref)) next.delete(ref);
      else next.add(ref);
      return next;
    });
  }

  return (
    <div className="list-view">
      <header className="list-head">
        <div>
          <h1>Test suites</h1>
          <p className="muted">
            {plural(items.length, "suite")} · {plural(cases, "case")}
            {incomplete ? ` · ${incomplete} flagged incomplete` : ""}. Every case must link to a requirement; a suite that does not is stored but flagged.
          </p>
        </div>
        <button type="button" className="btn primary" onClick={() => desktop.openDialog("suite")}><Icon.Upload size={14} /> Import suite</button>
      </header>

      {items.length === 0 && (
        <EmptyState title="No test suites yet" icon={<Icon.Suite size={26} />}>
          <p>Import a suite document or generate one with <code>loomwork test-suite generate</code>.</p>
          <button type="button" className="btn primary" onClick={() => desktop.openDialog("suite")}><Icon.Upload size={14} /> Import a suite</button>
        </EmptyState>
      )}

      <div className="suite-list">
        {items.map((suite) => {
          const open = expanded.has(suite.ref);
          return (
            <section className={`suite-card ${open ? "open" : ""}`} key={suite.ref}>
              <div className="suite-head">
                <button type="button" className="suite-toggle" onClick={() => toggle(suite.ref)} aria-expanded={open} aria-label={open ? "Collapse" : "Expand"}>
                  <Icon.Chevron size={14} className={open ? "rot90" : ""} />
                </button>
                <button type="button" className="suite-title" onClick={() => toggle(suite.ref)}>
                  <b>{suite.name}</b>
                  <span className="mono muted">{suite.ref}</span>
                </button>
                <StatusBadge status={suite.status} />
                <span className="muted">{plural(suite.children?.length || 0, "case")}</span>
                <small className="mono muted">v{suite.version}</small>
                <button type="button" className="btn small ghost" onClick={() => desktop.openItem(suite)} title="Open in a tab">Open</button>
              </div>
              {open && <ExpandedSuite suite={suite} />}
            </section>
          );
        })}
      </div>
    </div>
  );
}

function ExpandedSuite({suite}: {suite: TreeItem}) {
  const {projectRef} = useDesktop();
  const document = useQuery({
    queryKey: ["item", projectRef, "test-suites", suite.ref],
    queryFn: () => api.projectItem(projectRef, "test-suites", suite.ref),
  });
  if (document.isLoading) return <div className="suite-body"><Spinner /></div>;
  if (document.error) return <div className="suite-body"><ErrorPanel error={document.error} /></div>;
  if (!document.data) return null;
  return (
    <div className="suite-body">
      <SuiteDetail suite={document.data.body as TestSuite} compact />
    </div>
  );
}
