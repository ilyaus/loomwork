import {useQuery} from "@tanstack/react-query";
import {api} from "../api";
import {useDesktop} from "../lib/desktop";
import {baseName} from "../lib/format";
import type {RequirementTestLink} from "../types";
import {Icon} from "./Icons";
import {Badge} from "./ui";

export function useRequirementTests() {
  const {projectRef} = useDesktop();
  return useQuery({queryKey: ["requirement-tests", projectRef], queryFn: () => api.requirementTests(projectRef)});
}

export const testLinkKey = (link: RequirementTestLink) => `${link.family}:${link.family === "artifacts" ? link.name : link.ref}`;

export default function RequirementTests({links, loading = false, compact = false, onSelect}: {links?: RequirementTestLink[]; loading?: boolean; compact?: boolean; onSelect?: (link: RequirementTestLink) => void}) {
  const desktop = useDesktop();
  const visible = compact ? links?.slice(0, 3) : links;
  function render(tests: RequirementTestLink[]) {
    return <ul className="requirement-test-list">
      {tests.map(link => <li key={`${link.family}:${link.ref}`}>
        <button type="button" className="requirement-test-link" title={link.name} onClick={() => onSelect ? onSelect(link) : desktop.openItem(link)}>
          {link.family === "test-cases" ? <Icon.Case size={14} /> : <Icon.Artifact size={14} />}
          <span>
            <b>{link.family === "artifacts" ? baseName(link.name) : link.name}</b>
            <small>{link.family === "test-cases" ? `Test case · ${link.suiteTitle || link.suiteId}` : "Markdown scenario"} · v{link.version}</small>
          </span>
          <Icon.Chevron size={12} />
        </button>
      </li>)}
    </ul>;
  }
  return <section className={`requirement-tests ${compact ? "compact" : ""}`} aria-label="Linked tests">
    <h3><Icon.Link size={13} /> Linked tests {links && <Badge>{links.length}</Badge>}</h3>
    {loading ? <p className="muted small">Loading test links…</p> : !links ? <p className="muted small">Test links unavailable</p> : links.length === 0 ? <p className="muted small">No linked tests</p> : <>
      {render(visible || [])}
      {compact && links.length > 3 && <details><summary>Show {links.length - 3} more</summary>{render(links.slice(3))}</details>}
    </>}
    {!compact && <p className="muted small">Current suite cases and scenario documents, not execution results.</p>}
  </section>;
}
