# UI prototypes

Three static, self-contained HTML mockups exploring what a Loomwork UI could look
like. No build step, no JS framework, mock data only — open any file directly in
a browser (start with `index.html`).

| File | Style | Focus |
|---|---|---|
| `01-console-dark.html` | Dark three-pane console (Linear-inspired) | Projects → artifacts → detail: pinning, lineage, run panel (provider/model/preset/cue), run history |
| `02-studio-light.html` | Light document-centric workspace (Notion-inspired) | Artifact-as-page: properties, content, inline run box, version/run timeline |
| `03-workbench-ide.html` | IDE-style workbench | sdd-qa generate→run→analyze→refine loop over `api-test-runner`, side-by-side spec/report, loop log |
| `04-workbench-app.html` | **Chosen direction** — interactive IDE-style app | Extends 03 into a multi-view app: activity rail (workbench / artifacts / cues / providers), clickable stages and terminal tabs, artifact browser with lineage navigation, cue library, provider + preset status |

These are throwaway explorations kept for reference. The `internal/server` +
`loomwork serve` implementation that once backed them has been discarded and
removed; the browser UI will be rebuilt over the typed QA domain entities
(projects, requirements, agent definitions, test suites, reports) described in
`docs/loom-work-vision.md`, so these mockups predate the current product
direction.
