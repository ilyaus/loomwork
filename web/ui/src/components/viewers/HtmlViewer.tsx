import {useState} from "react";
import type {ViewerProps} from ".";
import {TextViewer} from "./TextViewer";

// HtmlViewer renders artifact HTML inside a fully sandboxed iframe: no scripts,
// no same-origin access, no navigation.
export default function HtmlViewer(props: ViewerProps) {
  const [source, setSource] = useState(false);
  if (typeof props.document.body !== "string" || source) {
    return (
      <div className="stack">
        <div className="viewer-tools"><span className="spacer" /><button type="button" className="btn small ghost on" onClick={() => setSource(false)}>Preview</button></div>
        <TextViewer {...props} />
      </div>
    );
  }
  return (
    <div className="stack">
      <div className="viewer-tools">
        <span className="muted small">Sandboxed preview: scripts and navigation are blocked.</span>
        <span className="spacer" />
        <button type="button" className="btn small ghost" onClick={() => setSource(true)}>Source</button>
      </div>
      <iframe className="html-frame" srcDoc={props.document.body} sandbox="" title={props.document.name} />
    </div>
  );
}
