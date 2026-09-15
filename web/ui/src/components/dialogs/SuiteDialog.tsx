import {ChangeEvent, FormEvent, useMemo, useState} from "react";
import {useMutation} from "@tanstack/react-query";
import {api} from "../../api";
import {Icon} from "../Icons";
import {Dialog, ErrorPanel, Field} from "../ui";
import {useDesktop} from "../../lib/desktop";
import type {ImportResult} from "../../types";

const example = `{
  "suite_id": "suite-orders",
  "title": "Orders API",
  "origin": "imported",
  "cases": [
    {
      "name": "gets an existing order",
      "requirement_ids": ["req-001"],
      "overrides_applied": [],
      "scenario": "happy-path",
      "request": {"method": "GET", "path": "/orders/ord-1001"},
      "expected": {"status": 200, "body_fields": ["id", "total"]}
    }
  ]
}`;

export default function SuiteDialog({open, onClose}: {open: boolean; onClose: () => void}) {
  const desktop = useDesktop();
  const [source, setSource] = useState("");
  const [result, setResult] = useState<ImportResult | null>(null);
  const parsed = useMemo(() => {
    if (!source.trim()) return {document: null, error: ""};
    try {
      return {document: JSON.parse(source) as unknown, error: ""};
    } catch (error) {
      return {document: null, error: error instanceof Error ? error.message : "Invalid JSON"};
    }
  }, [source]);
  const importSuite = useMutation({
    mutationFn: () => api.importTestSuite(desktop.projectRef, parsed.document),
    onSuccess: (imported) => {
      setResult(imported);
      desktop.refresh();
    },
  });

  function pickFile(event: ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0];
    if (file) void file.text().then(setSource);
  }
  function submit(event: FormEvent) {
    event.preventDefault();
    if (parsed.document) importSuite.mutate();
  }
  function finish() {
    const suite = result?.suite;
    setResult(null);
    setSource("");
    onClose();
    if (suite) desktop.openItem({family: "test-suites", ref: suite.suite_id, name: suite.title || suite.suite_id, version: suite.version});
  }

  return (
    <Dialog open={open} title="Import test suite" onClose={result ? finish : onClose} width={680}>
      {result ? (
        <div className="form">
          <div className={result.suite.incomplete ? "warning-panel" : "success-panel"}>
            {result.suite.incomplete ? <Icon.Warning size={16} /> : <Icon.Check size={16} />}
            <div>
              <b>Stored {result.suite.suite_id} v{result.suite.version}</b> with {result.suite.cases?.length ?? result.suite.case_ids?.length ?? 0} cases.
              {result.suite.incomplete && (
                <>
                  {" "}It is flagged incomplete:
                  <ul>{result.suite.incomplete_reasons?.map((reason) => <li key={reason}>{reason}</li>)}</ul>
                </>
              )}
            </div>
          </div>
          {result.audit.findings?.filter((finding) => finding.kind === "annotated").length ? (
            <div className="notice">
              Override citations were added automatically:
              <ul>{result.audit.findings.filter((finding) => finding.kind === "annotated").map((finding) => <li key={`${finding.case_id}-${finding.rule_ref}`}>{finding.case_id}: {finding.detail}</li>)}</ul>
            </div>
          ) : null}
          <div className="form-actions"><button type="button" className="btn primary" onClick={finish}>Open the suite</button></div>
        </div>
      ) : (
        <form className="form" onSubmit={submit}>
          <p className="muted small">
            Paste a suite document matching <code>docs/schemas/test-case.schema.json</code>. Posting an existing <code>suite_id</code> writes its next version.
            Cases without a requirement link are stored but flag the suite incomplete.
          </p>
          <Field label="Suite JSON">
            <textarea value={source} onChange={(event) => setSource(event.target.value)} rows={14} className="mono" placeholder={example} spellCheck={false} />
          </Field>
          <div className="form-row">
            <label className="btn file-btn">Choose file…<input type="file" accept=".json,application/json" onChange={pickFile} hidden /></label>
            <button type="button" className="btn ghost" onClick={() => setSource(example)}>Use the example</button>
            {parsed.error && <span className="error-text">{parsed.error}</span>}
          </div>
          {importSuite.error && <ErrorPanel error={importSuite.error} />}
          <div className="form-actions">
            <button type="button" className="btn" onClick={onClose}>Cancel</button>
            <button type="submit" className="btn primary" disabled={importSuite.isPending || !parsed.document}>{importSuite.isPending ? "Importing…" : "Import suite"}</button>
          </div>
        </form>
      )}
    </Dialog>
  );
}
