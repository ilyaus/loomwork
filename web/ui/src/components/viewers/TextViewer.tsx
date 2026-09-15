import {useMemo, useState} from "react";
import type {ViewerProps} from ".";

export function bodyText(body: unknown): string {
  if (typeof body === "string") return body;
  return JSON.stringify(body, null, 2);
}

// TextViewer shows raw content with line numbers and a wrap toggle. JSON bodies
// are pretty-printed.
export function TextViewer({document}: ViewerProps) {
  const [wrap, setWrap] = useState(true);
  const text = useMemo(() => {
    const raw = bodyText(document.body);
    if (document.mediaType.startsWith("application/json") && typeof document.body === "string") {
      try {
        return JSON.stringify(JSON.parse(document.body), null, 2);
      } catch {
        return raw;
      }
    }
    return raw;
  }, [document.body, document.mediaType]);
  const lines = useMemo(() => text.split("\n"), [text]);
  return (
    <div className="text-viewer">
      <div className="viewer-tools">
        <span className="muted small">{lines.length} lines · {text.length.toLocaleString()} chars</span>
        <span className="spacer" />
        <button type="button" className={`btn small ghost ${wrap ? "on" : ""}`} onClick={() => setWrap((value) => !value)}>Wrap</button>
        <button type="button" className="btn small ghost" onClick={() => void navigator.clipboard?.writeText(text)}>Copy</button>
      </div>
      <pre className={`code-lines ${wrap ? "wrap" : ""}`}>
        {lines.map((line, index) => (
          <span className="code-line" key={index}><span className="ln">{index + 1}</span><span className="lc">{line || " "}</span></span>
        ))}
      </pre>
    </div>
  );
}

const levelPattern = /\b(TRACE|DEBUG|INFO|WARN(?:ING)?|ERROR|FATAL|PANIC)\b/;

// LogViewer colors lines by log level and lets you show only warnings and errors.
export function LogViewer({document}: ViewerProps) {
  const [onlyProblems, setOnlyProblems] = useState(false);
  const text = bodyText(document.body);
  const lines = useMemo(() => text.split("\n").map((line) => {
    const match = levelPattern.exec(line);
    const level = match ? match[1].toLowerCase().replace("warning", "warn") : "";
    return {line, level};
  }), [text]);
  const problems = lines.filter((entry) => ["warn", "error", "fatal", "panic"].includes(entry.level)).length;
  const visible = onlyProblems ? lines.filter((entry) => ["warn", "error", "fatal", "panic"].includes(entry.level)) : lines;
  return (
    <div className="text-viewer">
      <div className="viewer-tools">
        <span className="muted small">{lines.length} lines · {problems} warnings or errors</span>
        <span className="spacer" />
        <button type="button" className={`btn small ghost ${onlyProblems ? "on" : ""}`} onClick={() => setOnlyProblems((value) => !value)} disabled={problems === 0}>
          Problems only
        </button>
        <button type="button" className="btn small ghost" onClick={() => void navigator.clipboard?.writeText(text)}>Copy</button>
      </div>
      <pre className="code-lines wrap log">
        {visible.map((entry, index) => (
          <span className={`code-line level-${entry.level}`} key={index}><span className="ln">{index + 1}</span><span className="lc">{entry.line || " "}</span></span>
        ))}
      </pre>
    </div>
  );
}
