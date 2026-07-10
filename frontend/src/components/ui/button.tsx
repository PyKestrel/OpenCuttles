import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "@/lib/utils";

const button = cva(
  "inline-flex items-center justify-center gap-1.5 whitespace-nowrap rounded-lg font-medium transition-colors disabled:pointer-events-none disabled:opacity-50",
  {
    variants: {
      variant: {
        primary: "bg-[var(--primary-strong)] text-primary-foreground hover:opacity-90",
        secondary: "border bg-secondary text-foreground hover:bg-accent",
        ghost: "text-muted-foreground hover:bg-accent hover:text-foreground",
        danger: "text-white hover:opacity-90",
      },
      size: {
        sm: "px-2.5 py-1.5 text-[12.5px]",
        md: "px-3 py-2 text-[13px]",
      },
    },
    defaultVariants: { variant: "secondary", size: "md" },
  },
);

export type ButtonProps = React.ButtonHTMLAttributes<HTMLButtonElement> &
  VariantProps<typeof button>;

export function Button({ className, variant, size, style, ...props }: ButtonProps) {
  // The danger variant reads a semantic token that has no Tailwind bg-* utility.
  const dangerStyle = variant === "danger" ? { background: "var(--destructive)", ...style } : style;
  return <button className={cn(button({ variant, size }), className)} style={dangerStyle} {...props} />;
}
