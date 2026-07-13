import { useEffect, useRef, useState, type FormEvent } from "react";
import { useFlueAgent, useFlueClient } from "@flue/react";
import type { FlueConversationPart } from "@flue/react";
import { RotateCcw, Send, Sparkles, Square, Wrench } from "lucide-react";
import { cn } from "@/lib/utils";
import { api } from "@/api";
import type { Instance } from "@/types";

const AGENT_NAME = "opencuttles";

function AgentPart({ part }: { part: FlueConversationPart }) {
  if (part.type === "text") {
    return part.text ? <p className="whitespace-pre-wrap text-[13.5px] leading-relaxed">{part.text}</p> : null;
  }
  if (part.type === "reasoning") {
    return part.text ? <p className="whitespace-pre-wrap text-[12.5px] italic leading-relaxed text-muted-foreground/80">{part.text}</p> : null;
  }
  if (part.type === "file") {
    return <em className="text-[12px] text-muted-foreground">[attachment]</em>;
  }
  const anyPart = part as { type: string; toolName?: string; name?: string; state?: string };
  if (anyPart.type && anyPart.type.includes("tool")) {
    const name = (anyPart.toolName ?? anyPart.name ?? "tool").replace(/^mcp__oc__/, "");
    return (
      <div className="inline-flex items-center gap-1.5 rounded-md border px-2 py-1 text-[12px] text-muted-foreground" style={{ borderColor: "var(--border-strong)", background: "var(--secondary)" }}>
        <Wrench className="size-3 text-primary" />
        <span className="font-mono">{name}</span>
        {anyPart.state ? <span className="text-muted-foreground/60">· {anyPart.state}</span> : null}
      </div>
    );
  }
  return null;
}

// Natural-language device driver: one Flue conversation thread per device.
export function AgentTab({ instance }: { instance: Instance }) {
  // A conversation is pinned to its id and keeps the model it was created with;
  // bumping the epoch starts a fresh thread that picks up the current model
  // (and escapes a spiralled one). Reset to the base thread when the device changes.
  const [epoch, setEpoch] = useState(0);
  const conversationId = epoch === 0 ? `oc-${instance.id}` : `oc-${instance.id}-r${epoch}`;
  const agent = useFlueAgent({ name: AGENT_NAME, id: conversationId });
  const client = useFlueClient();
  const [input, setInput] = useState("");
  const [stopping, setStopping] = useState(false);
  const [modelLabel, setModelLabel] = useState("");
  const logRef = useRef<HTMLDivElement>(null);

  const busy = agent.status === "submitted" || agent.status === "streaming";

  // Show the actually-configured model (admins). New conversations pick up the
  // latest choice; older threads keep the model they started with.
  useEffect(() => {
    api
      .agentModel()
      .then((c) => setModelLabel(c.providerId && c.model ? `${c.providerId}/${c.model}` : "local default"))
      .catch(() => setModelLabel(""));
  }, []);

  // Interrupt the in-flight run so the operator can revise the prompt. The Flue
  // client aborts the agent instance's current (and any queued) durable work;
  // the live conversation stream settles it to the aborted outcome.
  async function stop() {
    setStopping(true);
    try {
      await client.agents.abort(AGENT_NAME, conversationId);
    } catch {
      // Surfaced via agent.error.
    } finally {
      setStopping(false);
    }
  }

  useEffect(() => setEpoch(0), [instance.id]);

  useEffect(() => {
    logRef.current?.scrollTo({ top: logRef.current.scrollHeight });
  }, [agent.messages]);

  async function submit(event: FormEvent) {
    event.preventDefault();
    const message = input.trim();
    if (!message) return;
    setInput("");
    try {
      await agent.sendMessage(message);
    } catch {
      // Surfaced via agent.error.
    }
  }

  return (
    <div className="flex h-full min-h-[480px] flex-col overflow-hidden rounded-xl border bg-card" style={{ boxShadow: "var(--card-shadow)" }}>
      <div className="flex items-center gap-2.5 border-b px-4 py-3" style={{ borderColor: "var(--hairline)" }}>
        <span className="grid size-6 place-items-center rounded-md text-primary" style={{ background: "var(--brand-weak)" }}>
          <Sparkles className="size-3.5" />
        </span>
        <span className="text-[13px] font-semibold">Cognitive core</span>
        {modelLabel && <span className="truncate font-mono text-[11px] text-muted-foreground/70">{modelLabel}</span>}
        <button
          onClick={() => { setEpoch((e) => e + 1); setInput(""); }}
          title="New conversation (picks up the current model)"
          className="ml-auto grid size-7 place-items-center rounded-md text-muted-foreground hover:bg-accent hover:text-foreground"
        >
          <RotateCcw className="size-3.5" />
        </button>
        <span className="inline-flex items-center gap-1.5 text-[12px] text-muted-foreground">
          <span className="size-1.5 rounded-full" style={{ background: agent.status === "error" ? "var(--destructive)" : busy ? "var(--warn)" : "var(--running)" }} />
          {agent.status}
        </span>
      </div>

      {agent.error && (
        <div className="border-b px-4 py-2 text-[12.5px]" style={{ background: "color-mix(in srgb, var(--destructive) 10%, transparent)", color: "var(--destructive)" }}>
          {agent.error.message}
        </div>
      )}

      <div ref={logRef} className="flex-1 space-y-4 overflow-auto px-4 py-4">
        {agent.messages.length === 0 && (
          <div className="grid h-full place-items-center px-6 text-center text-[13px] text-muted-foreground/80">
            <p className="max-w-sm">
              Ask the agent to operate the device in natural language — e.g. “open Settings and turn on Airplane mode”, or “what apps are installed?”.
            </p>
          </div>
        )}
        {agent.messages.map((message) => (
          <div key={message.id} className={cn("flex flex-col gap-1.5", message.role === "user" && "items-end")}>
            <span className="text-[10.5px] uppercase tracking-[0.06em] text-muted-foreground/70">{message.role}</span>
            <div
              className={cn(
                "max-w-[85%] space-y-1.5 rounded-xl px-3.5 py-2.5",
                message.role === "user" ? "text-primary-foreground" : "border bg-secondary",
              )}
              style={message.role === "user" ? { background: "var(--primary)" } : { borderColor: "var(--border)" }}
            >
              {message.parts.map((part, i) => (
                <AgentPart key={i} part={part} />
              ))}
            </div>
          </div>
        ))}
      </div>

      <form className="flex gap-2 border-t p-3" onSubmit={submit} style={{ borderColor: "var(--hairline)" }}>
        <input
          value={input}
          onChange={(e) => setInput(e.target.value)}
          placeholder={busy ? "Stop to revise the prompt…" : "Tell the agent what to do…"}
          className="min-w-0 flex-1 rounded-lg border bg-secondary px-3.5 py-2.5 text-[13.5px] outline-none focus:border-[var(--ring)]"
        />
        {busy ? (
          <button
            type="button"
            onClick={stop}
            disabled={stopping}
            className="inline-flex items-center gap-1.5 rounded-lg px-3.5 py-2.5 text-[13px] font-medium text-white disabled:opacity-50"
            style={{ background: "var(--destructive)" }}
          >
            <Square className="size-3.5" fill="currentColor" stroke="none" /> {stopping ? "Stopping…" : "Stop"}
          </button>
        ) : (
          <button
            disabled={!input.trim()}
            className="inline-flex items-center gap-1.5 rounded-lg px-3.5 py-2.5 text-[13px] font-medium text-primary-foreground disabled:opacity-50"
            style={{ background: "var(--primary-strong)" }}
          >
            <Send className="size-3.5" /> Send
          </button>
        )}
      </form>
    </div>
  );
}
