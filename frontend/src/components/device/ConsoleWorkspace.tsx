import { cn } from "@/lib/utils";
import { ControlsTab } from "@/components/device/ControlsTab";
import { AgentTab } from "@/components/device/AgentTab";
import type { Instance } from "@/types";

export type ConsolePane = "controls" | "agent";

// The device operation workspace: the live WebRTC console stays visible on the
// left while the operator switches between manual Controls and the natural-
// language Agent on the right. Mirrors the old side-by-side console layout.
export function ConsoleWorkspace({
  instance,
  canControl,
  pane,
  onPane,
}: {
  instance: Instance;
  canControl: boolean;
  pane: ConsolePane;
  onPane: (p: ConsolePane) => void;
}) {
  const running = instance.state === "running";

  return (
    <div className="flex flex-col gap-4 xl:h-[720px] xl:flex-row">
      {/* live console */}
      <div
        className={cn(
          "min-h-[440px] overflow-hidden rounded-xl border bg-black xl:min-h-0",
          canControl ? "xl:flex-[1.25]" : "xl:flex-1",
        )}
        style={{ borderColor: "var(--border-strong)" }}
      >
        {running ? (
          <iframe
            title={`${instance.name} console`}
            src={instance.consoleUrl}
            allow="autoplay; microphone; camera; clipboard-write"
            className="size-full"
          />
        ) : (
          <div className="grid size-full place-items-center bg-secondary/40 px-6 text-center text-[13.5px] text-muted-foreground">
            <p className="max-w-xs">Start the device to open its interactive console.</p>
          </div>
        )}
      </div>

      {/* controls / agent */}
      {canControl && (
        <div className="flex min-h-0 flex-col xl:flex-1">
          <div className="mb-3 inline-flex self-start rounded-lg border p-0.5" style={{ background: "var(--secondary)" }}>
            {(["controls", "agent"] as ConsolePane[]).map((p) => (
              <button
                key={p}
                onClick={() => onPane(p)}
                className={cn(
                  "rounded-md px-4 py-1.5 text-[13px] font-medium capitalize transition-colors",
                  pane === p ? "text-foreground" : "text-muted-foreground hover:text-foreground",
                )}
                style={pane === p ? { background: "var(--card)", boxShadow: "var(--card-shadow)" } : undefined}
              >
                {p}
              </button>
            ))}
          </div>
          <div className="min-h-0 flex-1 xl:overflow-auto">
            {pane === "controls" ? <ControlsTab instance={instance} /> : <AgentTab instance={instance} />}
          </div>
        </div>
      )}
    </div>
  );
}
