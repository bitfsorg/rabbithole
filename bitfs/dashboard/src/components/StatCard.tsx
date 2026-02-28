import type { LucideIcon } from "lucide-react";

interface StatCardProps {
  label: string;
  value: string | number;
  icon?: LucideIcon;
  mono?: boolean;
}

export function StatCard({ label, value, icon: Icon, mono }: StatCardProps) {
  return (
    <div className="rounded-lg border border-border bg-bg-card p-4">
      <div className="flex items-center gap-2 text-sm text-text-secondary">
        {Icon && <Icon className="h-4 w-4" />}
        {label}
      </div>
      <div
        className={`mt-2 text-xl font-semibold ${mono ? "font-mono text-base" : ""}`}
      >
        {value}
      </div>
    </div>
  );
}
