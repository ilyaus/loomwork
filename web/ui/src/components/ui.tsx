import {ReactNode, useEffect, useId, useRef} from "react";
import {Icon} from "./Icons";

// tone maps a domain status to a badge color.
export function statusTone(status?: string): string {
  switch (status) {
    case "active": case "ready": case "passed": case "available": return "ok";
    case "obsolete": case "superseded": case "skipped": return "muted";
    case "incomplete": case "failed": return "warn";
    default: return "neutral";
  }
}

export function Badge({tone = "neutral", children, title}: {tone?: string; children: ReactNode; title?: string}) {
  return <span className={`badge badge-${tone}`} title={title}>{children}</span>;
}

export function StatusBadge({status}: {status?: string}) {
  if (!status) return null;
  return <Badge tone={statusTone(status)}>{status}</Badge>;
}

export function Chips({items, tone, empty, onClick}: {
  items?: string[];
  tone?: string;
  empty?: string;
  onClick?: (value: string) => void;
}) {
  if (!items?.length) return empty ? <span className="muted">{empty}</span> : null;
  return (
    <div className="chips">
      {items.map((item) => onClick
        ? <button type="button" className={`chip chip-${tone || "neutral"} chip-link`} key={item} onClick={() => onClick(item)}>{item}</button>
        : <span className={`chip chip-${tone || "neutral"}`} key={item}>{item}</span>)}
    </div>
  );
}

export function EmptyState({title, children, icon}: {title: string; children?: ReactNode; icon?: ReactNode}) {
  return (
    <div className="empty-state">
      {icon && <div className="empty-icon">{icon}</div>}
      <b>{title}</b>
      {children && <div className="empty-body">{children}</div>}
    </div>
  );
}

export function ErrorPanel({error}: {error: unknown}) {
  const message = error instanceof Error ? error.message : String(error);
  return <div className="error-panel" role="alert"><Icon.Warning size={15} /> <span>{message}</span></div>;
}

export function Spinner({label = "Loading…"}: {label?: string}) {
  return <div className="spinner-row"><span className="spinner" aria-hidden="true" /> {label}</div>;
}

export function Field({label, hint, children, inline = false}: {label: string; hint?: string; children: ReactNode; inline?: boolean}) {
  return (
    <label className={`field ${inline ? "field-inline" : ""}`}>
      <span className="field-label">{label}</span>
      {children}
      {hint && <span className="field-hint">{hint}</span>}
    </label>
  );
}

export function KeyValue({rows}: {rows: [string, ReactNode][]}) {
  return (
    <dl className="kv">
      {rows.filter(([, value]) => value !== undefined && value !== null && value !== "").map(([key, value]) => (
        <div key={key}><dt>{key}</dt><dd>{value}</dd></div>
      ))}
    </dl>
  );
}

// Dialog is a modal built on <dialog> so focus trapping and Escape come from the
// browser.
export function Dialog({open, title, onClose, children, width = 560}: {
  open: boolean;
  title: string;
  onClose: () => void;
  children: ReactNode;
  width?: number;
}) {
  const ref = useRef<HTMLDialogElement>(null);
  const titleId = useId();
  useEffect(() => {
    const element = ref.current;
    if (!element) return;
    if (open && !element.open) element.showModal();
    if (!open && element.open) element.close();
  }, [open]);
  return (
    <dialog
      ref={ref}
      className="dialog"
      style={{width}}
      aria-labelledby={titleId}
      onClose={onClose}
      onClick={(event) => { if (event.target === ref.current) onClose(); }}
    >
      <div className="dialog-inner">
        <header className="dialog-head">
          <h2 id={titleId}>{title}</h2>
          <button type="button" className="icon-btn" onClick={onClose} aria-label="Close"><Icon.Close size={16} /></button>
        </header>
        {open && children}
      </div>
    </dialog>
  );
}

export function SafeLink({value, className}: {value: string; className?: string}) {
  try {
    const parsed = new URL(value);
    if (parsed.protocol === "http:" || parsed.protocol === "https:") {
      return <a href={parsed.href} target="_blank" rel="noreferrer" className={className}>{value}</a>;
    }
  } catch {
    // Not a URL: render as inert text below.
  }
  return <span className={`mono ${className || ""}`}>{value}</span>;
}
