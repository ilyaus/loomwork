import type {SVGProps} from "react";

type IconProps = SVGProps<SVGSVGElement> & {size?: number};

function base({size = 16, ...props}: IconProps) {
  return {
    width: size,
    height: size,
    viewBox: "0 0 24 24",
    fill: "none",
    stroke: "currentColor",
    strokeWidth: 1.8,
    strokeLinecap: "round" as const,
    strokeLinejoin: "round" as const,
    "aria-hidden": true,
    ...props,
  };
}

export const Icon = {
  Home: (p: IconProps) => <svg {...base(p)}><path d="M3 11.5 12 4l9 7.5" /><path d="M5 10v10h14V10" /></svg>,
  Requirement: (p: IconProps) => <svg {...base(p)}><path d="M9 6h11M9 12h11M9 18h11" /><path d="m4 6 1 1 2-2M4 12l1 1 2-2M4 18l1 1 2-2" /></svg>,
  Agent: (p: IconProps) => <svg {...base(p)}><rect x="4" y="7" width="16" height="12" rx="2" /><path d="M12 3v4M8 12h.01M16 12h.01M9 16h6" /></svg>,
  Rule: (p: IconProps) => <svg {...base(p)}><path d="M12 3 4 6v6c0 4.4 3.4 7.7 8 9 4.6-1.3 8-4.6 8-9V6z" /><path d="m9 12 2 2 4-4" /></svg>,
  Suite: (p: IconProps) => <svg {...base(p)}><path d="M9 3h6l1 3h3v15H5V6h3z" /><path d="m9 13 2 2 4-4" /></svg>,
  Case: (p: IconProps) => <svg {...base(p)}><path d="M9 3v7l-5 8a2 2 0 0 0 2 3h12a2 2 0 0 0 2-3l-5-8V3" /><path d="M8 3h8" /></svg>,
  Report: (p: IconProps) => <svg {...base(p)}><path d="M4 20V10M10 20V4M16 20v-7M22 20H2" /></svg>,
  Artifact: (p: IconProps) => <svg {...base(p)}><path d="M14 3H6a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V9z" /><path d="M14 3v6h6" /></svg>,
  Chevron: (p: IconProps) => <svg {...base(p)}><path d="m9 6 6 6-6 6" /></svg>,
  Close: (p: IconProps) => <svg {...base(p)}><path d="M6 6l12 12M18 6 6 18" /></svg>,
  Plus: (p: IconProps) => <svg {...base(p)}><path d="M12 5v14M5 12h14" /></svg>,
  Search: (p: IconProps) => <svg {...base(p)}><circle cx="11" cy="11" r="7" /><path d="m20 20-3.5-3.5" /></svg>,
  Chat: (p: IconProps) => <svg {...base(p)}><path d="M4 5h16v11H9l-5 4z" /></svg>,
  Send: (p: IconProps) => <svg {...base(p)}><path d="M4 12 20 4l-4 16-4-7z" /></svg>,
  Stop: (p: IconProps) => <svg {...base(p)}><rect x="6" y="6" width="12" height="12" rx="2" /></svg>,
  Sun: (p: IconProps) => <svg {...base(p)}><circle cx="12" cy="12" r="4" /><path d="M12 2v2M12 20v2M2 12h2M20 12h2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4" /></svg>,
  Moon: (p: IconProps) => <svg {...base(p)}><path d="M20 14.5A8 8 0 1 1 9.5 4a6.5 6.5 0 0 0 10.5 10.5z" /></svg>,
  Monitor: (p: IconProps) => <svg {...base(p)}><rect x="3" y="4" width="18" height="12" rx="2" /><path d="M8 20h8M12 16v4" /></svg>,
  Link: (p: IconProps) => <svg {...base(p)}><path d="M10 14a4 4 0 0 0 5.7 0l3-3a4 4 0 0 0-5.7-5.7l-1 1" /><path d="M14 10a4 4 0 0 0-5.7 0l-3 3a4 4 0 0 0 5.7 5.7l1-1" /></svg>,
  Pin: (p: IconProps) => <svg {...base(p)}><path d="M9 4h6l-1 6 3 3H7l3-3z" /><path d="M12 13v7" /></svg>,
  History: (p: IconProps) => <svg {...base(p)}><path d="M3 12a9 9 0 1 0 3-6.7" /><path d="M3 4v5h5" /><path d="M12 7v5l3 2" /></svg>,
  Warning: (p: IconProps) => <svg {...base(p)}><path d="M12 3 2 21h20z" /><path d="M12 10v5M12 18h.01" /></svg>,
  Check: (p: IconProps) => <svg {...base(p)}><path d="m5 12 5 5L20 7" /></svg>,
  Upload: (p: IconProps) => <svg {...base(p)}><path d="M12 16V4M6 10l6-6 6 6" /><path d="M4 20h16" /></svg>,
  Trash: (p: IconProps) => <svg {...base(p)}><path d="M4 7h16M9 7V4h6v3M6 7l1 13h10l1-13" /></svg>,
  Sidebar: (p: IconProps) => <svg {...base(p)}><rect x="3" y="4" width="18" height="16" rx="2" /><path d="M9 4v16" /></svg>,
  Book: (p: IconProps) => <svg {...base(p)}><path d="M4 4h6a2 2 0 0 1 2 2v14a2 2 0 0 0-2-2H4z" /><path d="M20 4h-6a2 2 0 0 0-2 2v14a2 2 0 0 1 2-2h6z" /></svg>,
  Layers: (p: IconProps) => <svg {...base(p)}><path d="m12 3 9 5-9 5-9-5z" /><path d="m3 13 9 5 9-5" /></svg>,
};

// familyIcon maps an entity family or artifact type to its glyph.
export function familyIcon(family: string, artifactType?: string) {
  switch (family) {
    case "requirements": return Icon.Requirement;
    case "agent-definitions": return Icon.Agent;
    case "override-rules": return Icon.Rule;
    case "test-suites": return Icon.Suite;
    case "test-cases": return Icon.Case;
    case "reports": return Icon.Report;
    default: return artifactType === "test-result" ? Icon.Report : Icon.Artifact;
  }
}
