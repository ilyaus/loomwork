import {FormEvent, useEffect, useState} from "react";
import {useMutation} from "@tanstack/react-query";
import {api} from "../../api";
import {Dialog, ErrorPanel, Field} from "../ui";
import {splitTags} from "../../lib/format";
import {useDesktop} from "../../lib/desktop";
import type {HTTPMethod, OverrideActionKind, OverrideRule, OverrideRuleWrite, TestScenario} from "../../types";

const methods: HTTPMethod[] = ["GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"];
const scenarios: TestScenario[] = ["happy-path", "missing-item", "invalid-input", "missing-authentication", "unauthorized", "conflict", "rate-limit", "server-error", "other"];

// RuleDialog creates an override rule or, when existing is given, writes its
// next version. The condition is optional; the rationale is not.
export default function RuleDialog({open, onClose, existing}: {open: boolean; onClose: () => void; existing?: OverrideRule}) {
  const desktop = useDesktop();
  const [id, setId] = useState("");
  const [title, setTitle] = useState("");
  const [selectedMethods, setSelectedMethods] = useState<HTTPMethod[]>([]);
  const [pathPattern, setPathPattern] = useState("");
  const [scenario, setScenario] = useState<TestScenario | "">("");
  const [specStatus, setSpecStatus] = useState("");
  const [kind, setKind] = useState<OverrideActionKind>("expect-status");
  const [expectStatus, setExpectStatus] = useState("200");
  const [rationale, setRationale] = useState("");
  const [tags, setTags] = useState("");

  useEffect(() => {
    if (!open) return;
    setId(existing?.id || "");
    setTitle(existing?.title || "");
    setSelectedMethods(existing?.condition?.methods || []);
    setPathPattern(existing?.condition?.path_pattern || "");
    setScenario(existing?.condition?.scenario || "");
    setSpecStatus(existing?.condition?.spec_status ? String(existing.condition.spec_status) : "");
    setKind(existing?.action?.kind || "expect-status");
    setExpectStatus(existing?.action?.expect_status ? String(existing.action.expect_status) : "200");
    setRationale(existing?.rationale || "");
    setTags(existing?.tags?.join(", ") || "");
  }, [open, existing]);

  const save = useMutation({
    mutationFn: () => {
      const write: OverrideRuleWrite = {
        title: title.trim(),
        rationale: rationale.trim(),
        tags: splitTags(tags),
        condition: {
          methods: selectedMethods.length ? selectedMethods : undefined,
          path_pattern: pathPattern.trim() || undefined,
          scenario: scenario || undefined,
          spec_status: specStatus ? Number(specStatus) : undefined,
        },
        action: kind === "skip-test" ? {kind} : {kind, expect_status: expectStatus ? Number(expectStatus) : undefined},
      };
      return existing
        ? api.updateOverrideRule(desktop.projectRef, existing.id, write)
        : api.createOverrideRule(desktop.projectRef, {id: id.trim(), ...write});
    },
    onSuccess: (rule) => {
      desktop.refresh();
      onClose();
      desktop.openItem({family: "override-rules", ref: rule.id, name: rule.title, version: rule.version, artifactType: "override-rule", status: rule.status});
    },
  });

  function toggleMethod(method: HTTPMethod) {
    setSelectedMethods((current) => current.includes(method) ? current.filter((value) => value !== method) : [...current, method]);
  }
  function submit(event: FormEvent) {
    event.preventDefault();
    save.mutate();
  }

  return (
    <Dialog open={open} title={existing ? `New version of ${existing.id}` : "New override rule"} onClose={onClose} width={720}>
      <form className="form" onSubmit={submit}>
        {existing
          ? <div className="form-note">Version {existing.version + 1} becomes active and v{existing.version} is retained as superseded. Suites that cited v{existing.version} keep that citation.</div>
          : <p className="muted small">An override rule tells the generator where a business rule beats the literal spec. The condition selects cases mechanically; the rationale lets the agent generalize.</p>}
        <div className="form-row">
          <Field label="Rule id" hint="Lowercase letters, digits, dashes. Cited by tests as id-vN.">
            <input value={id} onChange={(event) => setId(event.target.value)} pattern="[a-z0-9][a-z0-9\-]*" placeholder="missing-is-empty" required disabled={Boolean(existing)} autoFocus={!existing} />
          </Field>
          <Field label="Title">
            <input value={title} onChange={(event) => setTitle(event.target.value)} placeholder="Missing collections return an empty list" required />
          </Field>
        </div>

        <fieldset className="fieldset">
          <legend>When a test case matches</legend>
          <Field label="Methods" hint="Leave all unchecked to match any method.">
            <div className="method-picks">
              {methods.map((method) => (
                <label className={`pick ${selectedMethods.includes(method) ? "on" : ""}`} key={method}>
                  <input type="checkbox" checked={selectedMethods.includes(method)} onChange={() => toggleMethod(method)} />
                  {method}
                </label>
              ))}
            </div>
          </Field>
          <div className="form-row">
            <Field label="Path pattern" hint="* matches one segment, ** matches across segments">
              <input value={pathPattern} onChange={(event) => setPathPattern(event.target.value)} placeholder="/orders/*" className="mono" />
            </Field>
            <Field label="Scenario">
              <select value={scenario} onChange={(event) => setScenario(event.target.value as TestScenario | "")}>
                <option value="">Any scenario</option>
                {scenarios.map((value) => <option value={value} key={value}>{value}</option>)}
              </select>
            </Field>
            <Field label="Spec says" hint="The literal status this rule corrects (documentary).">
              <input value={specStatus} onChange={(event) => setSpecStatus(event.target.value)} inputMode="numeric" pattern="[1-5][0-9]{2}" placeholder="404" />
            </Field>
          </div>
        </fieldset>

        <fieldset className="fieldset">
          <legend>Then the expected behavior is</legend>
          <div className="form-row">
            <Field label="Action">
              <select value={kind} onChange={(event) => setKind(event.target.value as OverrideActionKind)}>
                <option value="expect-status">Expect a specific status</option>
                <option value="expect-empty-collection">Expect an empty collection (2xx)</option>
                <option value="skip-test">Do not test this case</option>
              </select>
            </Field>
            {kind !== "skip-test" && (
              <Field label="Expected status" hint={kind === "expect-empty-collection" ? "2xx; defaults to 200" : "100 to 599"}>
                <input value={expectStatus} onChange={(event) => setExpectStatus(event.target.value)} inputMode="numeric" pattern="[1-5][0-9]{2}" required={kind === "expect-status"} />
              </Field>
            )}
          </div>
        </fieldset>

        <Field label="Rationale" hint="Required. The business reason, written so an agent can apply it to cases the condition does not name.">
          <textarea value={rationale} onChange={(event) => setRationale(event.target.value)} rows={4} required placeholder="The storefront represents a missing order collection as an empty 200 list so clients never special-case 404." />
        </Field>
        <Field label="Tags" hint="Comma-separated">
          <input value={tags} onChange={(event) => setTags(event.target.value)} />
        </Field>
        {save.error && <ErrorPanel error={save.error} />}
        <div className="form-actions">
          <button type="button" className="btn" onClick={onClose}>Cancel</button>
          <button type="submit" className="btn primary" disabled={save.isPending || !title.trim() || !rationale.trim() || (!existing && !id.trim())}>
            {save.isPending ? "Saving…" : existing ? "Save new version" : "Create rule"}
          </button>
        </div>
      </form>
    </Dialog>
  );
}
