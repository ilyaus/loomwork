import {Fragment, ReactNode} from "react";

// Markdown renders a practical subset of CommonMark/GFM (headings, lists, fenced
// code, quotes, tables, emphasis, inline code, http(s) links) straight into React
// elements. Nothing is ever injected as HTML, so untrusted artifact and model
// text is safe to render.

type Block =
  | {kind: "heading"; level: number; text: string}
  | {kind: "paragraph"; text: string}
  | {kind: "code"; lang: string; text: string}
  | {kind: "quote"; text: string}
  | {kind: "list"; ordered: boolean; items: string[]}
  | {kind: "table"; header: string[]; rows: string[][]}
  | {kind: "rule"};

const fence = /^```\s*([\w+-]*)\s*$/;
const heading = /^(#{1,6})\s+(.*)$/;
const bullet = /^\s*[-*+]\s+(.*)$/;
const numbered = /^\s*\d+[.)]\s+(.*)$/;
const tableRow = /^\s*\|.*\|\s*$/;
const tableSeparator = /^\s*\|?\s*:?-{2,}:?\s*(\|\s*:?-{2,}:?\s*)*\|?\s*$/;

function splitCells(line: string): string[] {
  return line.trim().replace(/^\|/, "").replace(/\|$/, "").split("|").map((cell) => cell.trim());
}

export function parseMarkdown(source: string): Block[] {
  const lines = source.replace(/\r\n?/g, "\n").split("\n");
  const blocks: Block[] = [];
  let index = 0;
  while (index < lines.length) {
    const line = lines[index];
    if (!line.trim()) { index++; continue; }

    const fenceMatch = fence.exec(line);
    if (fenceMatch) {
      const buffer: string[] = [];
      index++;
      while (index < lines.length && !fence.test(lines[index])) buffer.push(lines[index++]);
      index++;
      blocks.push({kind: "code", lang: fenceMatch[1], text: buffer.join("\n")});
      continue;
    }
    const headingMatch = heading.exec(line);
    if (headingMatch) {
      blocks.push({kind: "heading", level: headingMatch[1].length, text: headingMatch[2].trim()});
      index++;
      continue;
    }
    if (/^\s*(-{3,}|\*{3,}|_{3,})\s*$/.test(line)) { blocks.push({kind: "rule"}); index++; continue; }
    if (line.startsWith(">")) {
      const buffer: string[] = [];
      while (index < lines.length && lines[index].startsWith(">")) buffer.push(lines[index++].replace(/^>\s?/, ""));
      blocks.push({kind: "quote", text: buffer.join("\n")});
      continue;
    }
    if (tableRow.test(line) && index + 1 < lines.length && tableSeparator.test(lines[index + 1])) {
      const header = splitCells(line);
      index += 2;
      const rows: string[][] = [];
      while (index < lines.length && tableRow.test(lines[index])) rows.push(splitCells(lines[index++]));
      blocks.push({kind: "table", header, rows});
      continue;
    }
    const isBullet = bullet.test(line);
    if (isBullet || numbered.test(line)) {
      const pattern = isBullet ? bullet : numbered;
      const items: string[] = [];
      while (index < lines.length) {
        const match = pattern.exec(lines[index]);
        if (match) { items.push(match[1]); index++; continue; }
        // A continuation line indented under the previous item.
        if (items.length && /^\s{2,}\S/.test(lines[index])) { items[items.length - 1] += " " + lines[index].trim(); index++; continue; }
        break;
      }
      blocks.push({kind: "list", ordered: !isBullet, items});
      continue;
    }
    const buffer: string[] = [];
    while (index < lines.length && lines[index].trim() && !heading.test(lines[index]) && !fence.test(lines[index])
      && !bullet.test(lines[index]) && !numbered.test(lines[index]) && !lines[index].startsWith(">")) {
      buffer.push(lines[index++]);
    }
    blocks.push({kind: "paragraph", text: buffer.join("\n")});
  }
  return blocks;
}

// Underscore emphasis is deliberately unsupported: identifiers such as
// requirement_ids are far more common in QA text than _italics_.
const inlinePattern = /(`[^`]+`)|(\*\*[^*]+\*\*)|(\*[^*\s][^*]*\*)|(\[[^\]]+\]\([^)\s]+\))|(https?:\/\/[^\s<>()]+)/g;

function safeHref(raw: string): string | null {
  try {
    const url = new URL(raw);
    return url.protocol === "http:" || url.protocol === "https:" ? url.href : null;
  } catch {
    return null;
  }
}

export function renderInline(text: string): ReactNode[] {
  const nodes: ReactNode[] = [];
  let last = 0;
  let key = 0;
  for (const match of text.matchAll(inlinePattern)) {
    const start = match.index ?? 0;
    if (start > last) nodes.push(text.slice(last, start));
    const token = match[0];
    if (match[1]) nodes.push(<code key={key++}>{token.slice(1, -1)}</code>);
    else if (match[2]) nodes.push(<strong key={key++}>{renderInline(token.slice(2, -2))}</strong>);
    else if (match[3]) nodes.push(<em key={key++}>{renderInline(token.slice(1, -1))}</em>);
    else if (match[4]) {
      const close = token.indexOf("](");
      const label = token.slice(1, close);
      const href = safeHref(token.slice(close + 2, -1));
      nodes.push(href
        ? <a key={key++} href={href} target="_blank" rel="noreferrer">{label}</a>
        : <span key={key++}>{label}</span>);
    } else if (match[5]) {
      const href = safeHref(token);
      nodes.push(href ? <a key={key++} href={href} target="_blank" rel="noreferrer">{token}</a> : token);
    }
    last = start + token.length;
  }
  if (last < text.length) nodes.push(text.slice(last));
  return nodes;
}

function renderText(text: string): ReactNode[] {
  return text.split("\n").flatMap((line, index, all) =>
    index < all.length - 1 ? [...renderInline(line), <br key={`br-${index}`} />] : renderInline(line));
}

export function Markdown({source, className}: {source: string; className?: string}) {
  const blocks = parseMarkdown(source);
  return (
    <div className={`markdown ${className || ""}`}>
      {blocks.map((block, index) => {
        switch (block.kind) {
          case "heading": {
            const Tag = `h${Math.min(6, block.level)}` as "h1" | "h2" | "h3" | "h4" | "h5" | "h6";
            return <Tag key={index}>{renderInline(block.text)}</Tag>;
          }
          case "code": return <pre key={index} className="code-block" data-lang={block.lang}><code>{block.text}</code></pre>;
          case "quote": return <blockquote key={index}>{renderText(block.text)}</blockquote>;
          case "rule": return <hr key={index} />;
          case "list": {
            const items = block.items.map((item, itemIndex) => <li key={itemIndex}>{renderInline(item)}</li>);
            return block.ordered ? <ol key={index}>{items}</ol> : <ul key={index}>{items}</ul>;
          }
          case "table": return (
            <table key={index}>
              <thead><tr>{block.header.map((cell, cellIndex) => <th key={cellIndex}>{renderInline(cell)}</th>)}</tr></thead>
              <tbody>
                {block.rows.map((row, rowIndex) => (
                  <tr key={rowIndex}>{row.map((cell, cellIndex) => <td key={cellIndex}>{renderInline(cell)}</td>)}</tr>
                ))}
              </tbody>
            </table>
          );
          default: return <p key={index}>{renderText(block.text)}</p>;
        }
      })}
      {blocks.length === 0 && <Fragment />}
    </div>
  );
}
