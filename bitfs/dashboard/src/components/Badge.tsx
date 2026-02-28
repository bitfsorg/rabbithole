const variants: Record<string, string> = {
  info: "text-text-secondary bg-bg-card-hover",
  warn: "text-warning bg-warning/10",
  error: "text-error bg-error/10",
  success: "text-success bg-success/10",
};

interface BadgeProps {
  variant: string;
  children: React.ReactNode;
}

export function Badge({ variant, children }: BadgeProps) {
  return (
    <span
      className={`inline-block rounded px-2 py-0.5 text-xs font-medium ${variants[variant] ?? variants.info}`}
    >
      {children}
    </span>
  );
}
