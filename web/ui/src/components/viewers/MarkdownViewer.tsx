import {useState} from "react";
import type {ViewerProps} from ".";
import {Markdown} from "../Markdown";
import {TextViewer, bodyText} from "./TextViewer";

export default function MarkdownViewer(props: ViewerProps) {
  const [raw, setRaw] = useState(false);
  const source = bodyText(props.document.body);
  const block = source.match(/^---\r?\n([\s\S]*?)\r?\n---(?:\r?\n|$)/);
  const frontmatter = block && /^[A-Za-z_][\w-]*\s*:/m.test(block[1]) ? block : null;
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
      <div className="doc-page">
        {frontmatter && <details className="markdown-frontmatter"><summary>Document metadata</summary><pre className="inline-code">{frontmatter[1]}</pre></details>}
        <Markdown source={frontmatter ? source.slice(frontmatter[0].length) : source} />
      </div>
    </div>
  );
}
