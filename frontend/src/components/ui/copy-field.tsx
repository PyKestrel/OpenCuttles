import { useState } from "react";
import { Check, Copy } from "lucide-react";
import { cn } from "@/lib/utils";

// A read-only value with a copy button. Used for one-time secrets (enrollment
// tokens, install one-liners) that the operator must move to another machine.
export function CopyField({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  const [copied, setCopied] = useState(false);
  function copy() {
    void navigator.clipboard?.writeText(value);
    setCopied(true);
    window.setTimeout(() => setCopied(false), 1500);
  }
  return (
    <div>
      <div className="mb-1 text-[12px] text-muted-foreground">{label}</div>
      <div className="flex items-stretch gap-2">
        <code className={cn("min-w-0 flex-1 overflow-x-auto rounded-lg border bg-secondary px-3 py-2 text-[12px] whitespace-nowrap", mono && "font-mono")} style={{ borderColor: "var(--border-strong)" }}>
          {value}
        </code>
        <button onClick={copy} title="Copy" aria-label={`Copy ${label}`} className="grid w-9 shrink-0 place-items-center rounded-lg border bg-secondary text-muted-foreground hover:bg-accent hover:text-foreground" style={{ borderColor: "var(--border-strong)" }}>
          {copied ? <Check className="size-3.5" style={{ color: "var(--running)" }} /> : <Copy className="size-3.5" />}
        </button>
      </div>
    </div>
  );
}
