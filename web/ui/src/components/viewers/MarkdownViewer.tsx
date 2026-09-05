import {useState} from "react";
import type {ViewerProps} from ".";
import {Markdown} from "../Markdown";
import {TextViewer, bodyText} from "./TextViewer";

export default function MarkdownViewer(props: ViewerProps) {
  const [raw, setRaw] = useState(false);
  if (raw) {
    return (
      <div className="stack">
        <div className="viewer-tools"><span className="spacer" /><button type="button" className="btn small ghost on" onClick={() => setRaw(false)}>Rendered</button></div>
        <TextViewer {...props} />
      </div>
    );
  }
  return (
    <div className="stack">
      <div className="viewer-tools"><span className="spacer" /><button type="button" className="btn small ghost" onClick={() => setRaw(true)}>Raw</button></div>
      <div className="doc-page"><Markdown source={bodyText(props.document.body)} /></div>
    </div>
  );
}
