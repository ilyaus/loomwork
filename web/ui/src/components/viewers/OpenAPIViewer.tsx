import {lazy, Suspense, useMemo, useState} from "react";
import type {ViewerProps} from ".";
import {Spinner} from "../ui";
import {TextViewer} from "./TextViewer";

const SwaggerUI = lazy(async () => {
  await import("../../swagger-ui-styles");
  return import("swagger-ui-react");
});

export function looksLikeOpenAPI(body: unknown): boolean {
  if (body && typeof body === "object") return "openapi" in body || "swagger" in body;
  if (typeof body !== "string") return false;
  try {
    const parsed = JSON.parse(body) as Record<string, unknown>;
    return Boolean(parsed) && typeof parsed === "object" && ("openapi" in parsed || "swagger" in parsed);
  } catch {
    return false;
  }
}

export default function OpenAPIViewer(props: ViewerProps) {
  const [source, setSource] = useState(false);
  const spec = useMemo(() => {
    if (typeof props.document.body !== "string") return props.document.body as object;
    try {
      return JSON.parse(props.document.body) as object;
    } catch {
      return null;
    }
  }, [props.document.body]);
  if (!spec || source) {
    return (
      <div className="stack">
        {spec && <div className="viewer-tools"><span className="spacer" /><button type="button" className="btn small ghost on" onClick={() => setSource(false)}>Rendered</button></div>}
        <TextViewer {...props} />
      </div>
    );
  }
  return (
    <div className="stack">
      <div className="viewer-tools">
        <span className="muted small">OpenAPI document. Try-it-out is disabled: Loomwork never calls the service.</span>
        <span className="spacer" />
        <button type="button" className="btn small ghost" onClick={() => setSource(true)}>Source</button>
      </div>
      <div className="swagger-viewer">
        <Suspense fallback={<Spinner label="Loading OpenAPI viewer…" />}>
          <SwaggerUI spec={spec} supportedSubmitMethods={[]} docExpansion="list" />
        </Suspense>
      </div>
    </div>
  );
}
