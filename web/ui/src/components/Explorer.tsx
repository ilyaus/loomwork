import {useMemo, useState} from "react";
import {Icon, familyIcon} from "./Icons";
import FileTree from "./FileTree";
import {familyActions, familyKey, itemKey, useDesktop} from "../lib/desktop";
import type {TreeGroup, TreeItem} from "../types";

// Families whose group label also opens a list view when toggling.
const listFamilies = new Set(Object.keys(familyActions));

export default function Explorer({groups, activeKey}: {groups: TreeGroup[]; activeKey: string}) {
  const desktop = useDesktop();
  const [filter, setFilter] = useState("");
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({requirements: true});

  const needle = filter.trim().toLowerCase();
  const matches = useMemo(() => {
    if (!needle) return null;
    const found: TreeItem[] = [];
    for (const group of groups) {
      for (const item of group.items) {
        if (item.name.toLowerCase().includes(needle) || item.ref.toLowerCase().includes(needle)) found.push(item);
        for (const child of item.children || []) {
          if (child.name.toLowerCase().includes(needle) || child.ref.toLowerCase().includes(needle)) found.push(child);
        }
      }
    }
    return found;
  }, [groups, needle]);

  const total = groups.reduce((count, group) => count + group.items.length, 0);

  return (
    <div className="explorer">
      <div className="explorer-head">
        <label className="search">
          <Icon.Search size={14} />
          <input value={filter} onChange={(event) => setFilter(event.target.value)} placeholder={`Filter ${total} items`} aria-label="Filter items" />
          {filter && <button type="button" className="icon-btn tiny" onClick={() => setFilter("")} aria-label="Clear filter"><Icon.Close size={12} /></button>}
        </label>
      </div>
      <button
        type="button"
        className={`tree-row tree-overview ${activeKey === "overview" ? "selected" : ""}`}
        onClick={desktop.openOverview}
      >
        <Icon.Home size={15} />
        <span className="tree-name">Overview</span>
      </button>
      <div className="tree-scroll">
        {matches ? (
          <div className="tree-group open">
            <div className="tree-group-head static"><span className="tree-name">{matches.length} matching</span></div>
            {matches.map((item) => <TreeNode item={item} activeKey={activeKey} key={itemKey(item.family, item.ref)} />)}
            {matches.length === 0 && <div className="tree-empty">Nothing matches "{filter}"</div>}
          </div>
        ) : groups.map((group) => (
          <Group
            group={group}
            open={!collapsed[group.family]}
            onToggle={() => setCollapsed((current) => ({...current, [group.family]: !current[group.family]}))}
            activeKey={activeKey}
            key={group.family}
          />
        ))}
      </div>
    </div>
  );
}

function Group({group, open, onToggle, activeKey}: {group: TreeGroup; open: boolean; onToggle: () => void; activeKey: string}) {
  const desktop = useDesktop();
  const GroupIcon = familyIcon(group.family);
  const isList = listFamilies.has(group.family);
  const selected = activeKey === familyKey(group.family);
  return (
    <section className={`tree-group ${open ? "open" : ""}`}>
      <div className={`tree-group-head ${selected ? "selected" : ""}`}>
        <button type="button" className="tree-toggle" onClick={onToggle} aria-label={open ? "Collapse" : "Expand"} aria-expanded={open}>
          <Icon.Chevron size={12} className={open ? "rot90" : ""} />
        </button>
        <button
          type="button"
          className="tree-group-label"
          onClick={() => {
            onToggle();
            if (isList) desktop.openFamily(group.family);
          }}
          aria-expanded={open}
          title={isList ? `Open ${group.label.toLowerCase()} list` : undefined}
        >
          <GroupIcon size={14} className={`fam fam-${group.family}`} />
          <span className="tree-name">{group.label}</span>
          <span className="count">{group.items.length}</span>
        </button>
        {familyActions[group.family] && !(group.family === "requirements" && desktop.requirementsReadOnly) && <button type="button" className="icon-btn tiny" onClick={() => desktop.openDialog(familyActions[group.family].kind)} aria-label={familyActions[group.family].label} title={familyActions[group.family].label}><Icon.Plus size={13} /></button>}
      </div>
      {open && (
        <div className="tree-children">
          {["reports", "artifacts"].includes(group.family) ? <FileNodes items={group.items} activeKey={activeKey} /> : group.items.map((item) => (
            <div key={itemKey(item.family, item.ref)}>
              {item.artifactType === "document-suite" ? <DocumentSuiteNode item={item} activeKey={activeKey} /> : <>
                <TreeNode item={item} activeKey={activeKey} />
                {item.children?.map((child) => <TreeNode item={child} activeKey={activeKey} nested key={itemKey(child.family, child.ref)} />)}
              </>}
            </div>
          ))}
          {group.items.length === 0 && <div className="tree-empty">None yet</div>}
        </div>
      )}
    </section>
  );
}

// FileNodes groups files by their folder so long paths and timestamped names
// stay readable.
function FileNodes({items, activeKey}: {items: TreeItem[]; activeKey: string}) {
  return items.length > 0 ? <FileTree items={items} family={items[0].family} activeKey={activeKey} compact /> : null;
}

function DocumentSuiteNode({item, activeKey}: {item: TreeItem; activeKey: string}) {
  const [open, setOpen] = useState(false);
  return <div>
    <div className="tree-suite-head">
      <button type="button" className="tree-toggle" aria-expanded={open} aria-label={`${open ? "Collapse" : "Expand"} ${item.name}`} onClick={() => setOpen(value => !value)}><Icon.Chevron size={12} className={open ? "rot90" : ""} /></button>
      <TreeNode item={item} activeKey={activeKey} ariaExpanded={open} onSelect={() => setOpen(value => !value)} />
    </div>
    {open && <FileTree items={item.children || []} family={`document-suites:${item.ref}`} activeKey={activeKey} compact />}
  </div>;
}

function TreeNode({item, activeKey, nested = false, ariaExpanded, onSelect}: {item: TreeItem; activeKey: string; nested?: boolean; ariaExpanded?: boolean; onSelect?: () => void}) {
  const desktop = useDesktop();
  const key = itemKey(item.family, item.ref);
  const NodeIcon = familyIcon(item.family, item.artifactType);
  return (
    <button
      type="button"
      className={`tree-row tree-node ${nested ? "nested" : ""} ${activeKey === key ? "selected" : ""}`}
      onClick={() => {
        desktop.openItem(item);
        onSelect?.();
      }}
      aria-expanded={ariaExpanded}
      title={item.name}
    >
      <NodeIcon size={14} className={`fam fam-${item.family} type-${item.artifactType}`} />
      <span className="tree-name">{item.name}</span>
      {item.status && item.status !== "active" && item.status !== "ready" && (
        <span className={`dot dot-${item.status}`} title={item.status} />
      )}
      {item.version ? <small className="mono">v{item.version}</small> : null}
    </button>
  );
}
