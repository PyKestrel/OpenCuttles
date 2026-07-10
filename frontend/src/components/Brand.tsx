import { cn } from "@/lib/utils";

/** The cuttlefish mark — the identity the dithered aesthetic derives from. */
export function BrandMark({ className }: { className?: string }) {
  return (
    <span
      className={cn(
        "grid place-items-center rounded-[7px] border text-primary",
        className,
      )}
      style={{ background: "var(--brand-weak)", borderColor: "color-mix(in srgb, var(--primary) 30%, transparent)" }}
    >
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" className="size-[62%]">
        <path d="M7 9.5C7 6.5 9.2 4.5 12 4.5s5 2 5 5c0 2-1 3-1 5.5 0 2.2 1.4 2.8 1.4 4.2M9 15c0 2-1.2 2.5-1.2 4M12 15v3.5M9 9.2h.01" />
      </svg>
    </span>
  );
}
