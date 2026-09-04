import {FormEvent, useEffect, useMemo, useRef, useState} from "react";
import {useQuery} from "@tanstack/react-query";
import {api, streamChat} from "../api";
import type {ChatMessage} from "../types";

type Props = {
  projectRef: string;
  activeArtifactRef?: string;
};

export default function ChatDock({projectRef, activeArtifactRef}: Props) {
  const models = useQuery({queryKey: ["models"], queryFn: api.models});
  const choices = useMemo(
    () => models.data?.flatMap((group) => group.models.flatMap((model) => [
      model.selector,
      ...model.presets.map((preset) => `${model.selector}#${preset}`),
    ])) || [],
    [models.data],
  );
  const [selector, setSelector] = useState("");
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [input, setInput] = useState("");
  const [streaming, setStreaming] = useState(false);
  const [error, setError] = useState("");
  const abortRef = useRef<AbortController | null>(null);
  const transcriptRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!selector && choices.length > 0) setSelector(choices[0]);
  }, [choices, selector]);
  useEffect(() => {
    transcriptRef.current?.scrollTo({top: transcriptRef.current.scrollHeight});
  }, [messages]);
  useEffect(() => () => abortRef.current?.abort(), []);

  async function submit(event: FormEvent) {
    event.preventDefault();
    const content = input.trim();
    if (!content || !selector || streaming) return;
    const userMessage: ChatMessage = {role: "user", content};
    const transcript = [...messages, userMessage];
    setMessages([...transcript, {role: "assistant", content: ""}]);
    setInput("");
    setError("");
    setStreaming(true);
    const controller = new AbortController();
    abortRef.current = controller;
    try {
      await streamChat(
        projectRef,
        selector,
        transcript,
        activeArtifactRef ? [activeArtifactRef] : [],
        ({event: eventName, data}) => {
          if (eventName === "message") {
            setMessages((current) => current.map((message, index) =>
              index === current.length - 1
                ? {...message, content: message.content + data.delta}
                : message,
            ));
          }
          if (eventName === "error") throw new Error(data.error);
        },
        controller.signal,
      );
    } catch (chatError) {
      if (!controller.signal.aborted) {
        setError(chatError instanceof Error ? chatError.message : "Chat failed");
      }
    } finally {
      setStreaming(false);
    }
  }

  return (
    <aside className="chat-dock">
      <div className="pane-h">
        <span className="tag tg-generated">agent</span>
        <b>Conversation</b>
      </div>
      <div className="chat-model">
        <label htmlFor="model-selector">Provider / model</label>
        <input
          id="model-selector"
          list="model-choices"
          value={selector}
          onChange={(event) => setSelector(event.target.value)}
          placeholder="ollama/model[#preset]"
        />
        <datalist id="model-choices">
          {choices.map((choice) => <option key={choice} value={choice} />)}
        </datalist>
        {models.data?.some((group) => group.status !== "available") && (
          <span className="model-warning" title={models.data.map((group) => `${group.provider}: ${group.status}`).join("\n")}>
            Some providers unavailable
          </span>
        )}
      </div>
      <div className="transcript" ref={transcriptRef}>
        {messages.length === 0 && (
          <div className="chat-empty">
            Ask about the project. When an artifact tab is active it is attached as context.
          </div>
        )}
        {messages.map((message, index) => (
          <div className={`message ${message.role}`} key={`${message.role}-${index}`}>
            <span>{message.role}</span>
            <p>{message.content || (streaming && index === messages.length - 1 ? "Thinking…" : "")}</p>
          </div>
        ))}
      </div>
      {error && <div className="chat-error">{error}</div>}
      <form className="chat-form" onSubmit={submit}>
        <textarea
          value={input}
          onChange={(event) => setInput(event.target.value)}
          placeholder="Message the project agent…"
          rows={3}
        />
        <button className="btn primary" disabled={streaming || !selector || !input.trim()}>
          {streaming ? "Streaming…" : "Send"}
        </button>
      </form>
    </aside>
  );
}
