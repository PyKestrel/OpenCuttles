import { cn } from "@/lib/utils";

export function Card({ className, children }: { className?: string; children: React.ReactNode }) {
  return (
    <div
      className={cn("overflow-hidden rounded-xl border bg-card text-card-foreground", className)}
      style={{ boxShadow: "var(--card-shadow)" }}
    >
      {children}
    </div>
  );
}

export function CardHeader({ icon, title, action }: { icon?: React.ReactNode; title: React.ReactNode; action?: React.ReactNode }) {
  return (
    <div className="flex items-center gap-2.5 border-b px-4 py-3 text-[13px] font-semibold" style={{ borderColor: "var(--hairline)" }}>
      {icon && <span className="text-muted-foreground">{icon}</span>}
      <span>{title}</span>
      {action && <span className="ml-auto">{action}</span>}
    </div>
  );
}
