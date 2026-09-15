import {FormEvent, KeyboardEvent, useEffect, useMemo, useRef, useState} from "react";
import {useQuery} from "@tanstack/react-query";
import {api, streamChat} from "../api";
import {Icon} from "./Icons";
import {Markdown} from "./Markdown";
import {Badge, ErrorPanel} from "./ui";
import type {ChatMessage} from "../types";

type Props = {
  projectRef: string;
  // activeArtifactRef is the artifact open in the viewer; only artifacts can be
  // attached as grounding context.
  activeArtifactRef?: string;
  activeLabel?: string;
  onClose: () => void;
};

type Turn = ChatMessage & {pending?: boolean; model?: string; finishReason?: string; tokens?: number};

const selectorKey = "loomwork.chatSelector";
const transcriptKey = (project: string) => `loomwork.chat.${project}`;

function readTranscript(project: string): Turn[] {
  try {
    const stored = JSON.parse(sessionStorage.getItem(transcriptKey(project)) || "[]") as Turn[];
    return stored.filter((turn) => !turn.pending);
  } catch {
    return [];
  }
}

export default function ChatDock({projectRef, activeArtifactRef, activeLabel, onClose}: Props) {
  const models = useQuery({queryKey: ["models"], queryFn: api.models, staleTime: 60_000});
  const [selector, setSelector] = useState(() => localStorage.getItem(selectorKey) || "");
  const [preset, setPreset] = useState("");
  const [attach, setAttach] = useState(true);
  const [messages, setMessages] = useState<Turn[]>(() => readTranscript(projectRef));
  const [input, setInput] = useState("");
  const [streaming, setStreaming] = useState(false);
  const [error, setError] = useState("");
  const abortRef = useRef<AbortController | null>(null);
  const transcriptRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLTextAreaElement>(null);

  const available = useMemo(() => (models.data || []).filter((group) => group.status === "available"), [models.data]);
  const chosen = useMemo(
    () => models.data?.flatMap((group) => group.models).find((model) => model.selector === selector),
    [models.data, selector],
  );

  // Pick the first model of the first available provider when nothing is chosen
  // or the remembered choice is gone.
  useEffect(() => {
    if (!models.data) return;
    const known = models.data.some((group) => group.models.some((model) => model.selector === selector));
    if (!known) setSelector(available[0]?.models[0]?.selector || "");
  }, [models.data, available, selector]);
  useEffect(() => { if (selector) localStorage.setItem(selectorKey, selector); }, [selector]);
  useEffect(() => { setPreset(""); }, [selector]);
  useEffect(() => { sessionStorage.setItem(transcriptKey(projectRef), JSON.stringify(messages)); }, [messages, projectRef]);
  useEffect(() => { transcriptRef.current?.scrollTo({top: transcriptRef.current.scrollHeight}); }, [messages]);
  useEffect(() => () => abortRef.current?.abort(), []);

  const fullSelector = preset ? `${selector}#${preset}` : selector;
  const attachedRefs = attach && activeArtifactRef ? [activeArtifactRef] : [];

  async function send() {
    const content = input.trim();
    if (!content || !selector || streaming) return;
    const transcript: ChatMessage[] = [...messages.map(({role, content: text}) => ({role, content: text})), {role: "user", content}];
    setMessages([...transcript, {role: "assistant", content: "", pending: true}]);
    setInput("");
    setError("");
    setStreaming(true);
    const controller = new AbortController();
    abortRef.current = controller;
    try {
      await streamChat(projectRef, fullSelector, transcript, attachedRefs, ({event, data}) => {
        if (event === "message") {
          setMessages((current) => current.map((turn, index) =>
            index === current.length - 1 ? {...turn, content: turn.content + data.delta, pending: false} : turn));
        } else if (event === "done") {
          setMessages((current) => current.map((turn, index) =>
            index === current.length - 1
              ? {...turn, pending: false, model: data.model, finishReason: data.finishReason, tokens: data.usage?.completionTokens}
              : turn));
        } else if (event === "error") {
          throw new Error(data.error);
        }
      }, controller.signal);
    } catch (chatError) {
      if (controller.signal.aborted) {
        setMessages((current) => current.map((turn, index) =>
          index === current.length - 1 ? {...turn, pending: false, content: turn.content || "(stopped)"} : turn));
      } else {
        setError(chatError instanceof Error ? chatError.message : "Chat failed");
        setMessages((current) => current.filter((turn, index) => !(index === current.length - 1 && turn.pending)));
      }
    } finally {
      setStreaming(false);
      inputRef.current?.focus();
    }
  }

  function submit(event: FormEvent) {
    event.preventDefault();
    void send();
  }
  function onKeyDown(event: KeyboardEvent<HTMLTextAreaElement>) {
    if (event.key === "Enter" && !event.shiftKey) {
      event.preventDefault();
      void send();
    }
  }
  function stop() {
    abortRef.current?.abort();
  }
  function clear() {
    stop();
    setMessages([]);
    setError("");
  }

  const noProviders = models.data && available.length === 0;

  return (
    <div className="chat">
      <header className="pane-head">
        <Icon.Chat size={15} />
        <b>Chat</b>
        <span className="spacer" />
        <button type="button" className="icon-btn" onClick={clear} title="Clear conversation" aria-label="Clear conversation" disabled={messages.length === 0}><Icon.Trash size={14} /></button>
        <button type="button" className="icon-btn" onClick={onClose} title="Hide chat" aria-label="Hide chat"><Icon.Close size={14} /></button>
      </header>

      <div className="chat-config">
        <label className="field">
          <span className="field-label">Model</span>
          <select value={selector} onChange={(event) => setSelector(event.target.value)} disabled={models.isLoading || !models.data?.length} aria-label="Model">
            {!models.data?.length && <option value="">{models.isLoading ? "Discovering providers…" : "No providers configured"}</option>}
            {models.data?.map((group) => (
              <optgroup label={`${group.provider}${group.status === "available" ? "" : " (unavailable)"}`} key={group.provider}>
                {group.models.map((model) => (
                  <option value={model.selector} key={model.selector} disabled={group.status !== "available"}>{model.id}</option>
                ))}
                {group.models.length === 0 && <option value="" disabled>no models</option>}
              </optgroup>
            ))}
          </select>
        </label>
        {chosen && chosen.presets.length > 0 && (
          <label className="field">
            <span className="field-label">Preset</span>
            <select value={preset} onChange={(event) => setPreset(event.target.value)} aria-label="Preset">
              <option value="">default parameters</option>
              {chosen.presets.map((name) => <option value={name} key={name}>{name}</option>)}
            </select>
          </label>
        )}
        {models.data?.some((group) => group.status !== "available") && (
          <details className="provider-status">
            <summary className="muted small">{models.data.filter((group) => group.status !== "available").length} provider(s) unavailable</summary>
            <ul>
              {models.data.filter((group) => group.status !== "available").map((group) => (
                <li key={group.provider}><b>{group.provider}</b>: <span className="muted">{group.status.replace(/^unavailable:\s*/, "")}</span></li>
              ))}
            </ul>
          </details>
        )}
        {models.error && <ErrorPanel error={models.error} />}
      </div>

      <div className="chat-context">
        {activeArtifactRef ? (
          <label className={`context-chip ${attach ? "on" : ""}`} title="Attach the open artifact as grounding context">
            <input type="checkbox" checked={attach} onChange={(event) => setAttach(event.target.checked)} />
            <Icon.Artifact size={13} />
            <span className="truncate">{activeLabel || activeArtifactRef}</span>
            <Badge tone={attach ? "ok" : "muted"}>{attach ? "attached" : "not attached"}</Badge>
          </label>
        ) : (
          <span className="muted small">Open an artifact tab to attach it as context{activeLabel ? ` (${activeLabel} is not an artifact)` : ""}.</span>
        )}
      </div>

      <div className="transcript" ref={transcriptRef}>
        {messages.length === 0 && (
          <div className="chat-empty">
            <Icon.Chat size={22} />
            <p>Ask about this project's requirements, specs, or logs. Replies are generated by the selected local or configured provider and are not stored.</p>
          </div>
        )}
        {messages.map((turn, index) => (
          <article className={`turn ${turn.role}`} key={`${turn.role}-${index}`}>
            <header>
              <span>{turn.role === "user" ? "You" : turn.model || "Assistant"}</span>
              {turn.finishReason && turn.finishReason !== "stop" && turn.finishReason !== "end_turn" && <Badge tone="warn">{turn.finishReason}</Badge>}
              {turn.tokens ? <span className="muted">{turn.tokens} tokens</span> : null}
            </header>
            {turn.pending && !turn.content
              ? <p className="thinking"><span className="spinner" /> Waiting for {chosen?.id || "the model"}…</p>
              : turn.role === "assistant"
                ? <Markdown source={turn.content} className="chat-md" />
                : <p className="user-text">{turn.content}</p>}
          </article>
        ))}
      </div>

      {error && <ErrorPanel error={error} />}
      {noProviders && <div className="notice">No provider is reachable. Start Ollama or LM Studio, or configure Azure or Bedrock credentials, then reload.</div>}

      <form className="composer" onSubmit={submit}>
        <textarea
          ref={inputRef}
          value={input}
          onChange={(event) => setInput(event.target.value)}
          onKeyDown={onKeyDown}
          placeholder={selector ? "Message the model… (Enter to send, Shift+Enter for a new line)" : "Select a model first"}
          rows={3}
          disabled={!selector}
        />
        <div className="composer-actions">
          <span className="muted small mono truncate" title={fullSelector}>{fullSelector}</span>
          {streaming
            ? <button type="button" className="btn danger" onClick={stop}><Icon.Stop size={14} /> Stop</button>
            : <button type="submit" className="btn primary" disabled={!selector || !input.trim()}><Icon.Send size={14} /> Send</button>}
        </div>
      </form>
    </div>
  );
}
