import type {ViewerProps} from ".";
import type {DocumentSuite} from "../../types";
import {useDesktop} from "../../lib/desktop";
import FileTree from "../FileTree";

export default function DocumentSuiteViewer({document}: ViewerProps) {
  const desktop = useDesktop();
  const suite = document.body as DocumentSuite;
  const items = suite.documents.map(artifact => ({
    ref: artifact.id, name: artifact.name.slice(suite.root.length + 1), family: "artifacts",
    artifactType: artifact.type, mediaType: artifact.body.mediaType || "text/plain", version: artifact.version,
  }));
  return <div className="list-view">
    <header className="list-head">
      <div><h1>{suite.name}</h1><p className="muted">{items.length} QA documents. Markdown suite, not executable test-suite JSON.</p></div>
      <button type="button" className="btn" onClick={() => desktop.openFamily("test-suites")}>Back to test suites</button>
    </header>
    <FileTree items={items} family={`document-suites:${suite.id}`} />
  </div>;
}
