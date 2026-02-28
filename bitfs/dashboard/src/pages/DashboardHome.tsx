import { Clock, HardDrive, Link } from "lucide-react";
import { StatCard } from "@/components/StatCard";
import { DataTable } from "@/components/DataTable";
import { Badge } from "@/components/Badge";
import { usePolling } from "@/hooks/usePolling";
import type { StatusResponse, StorageResponse, SaleRecord } from "@/lib/api";

function formatUptime(seconds: number): string {
  const d = Math.floor(seconds / 86400);
  const h = Math.floor((seconds % 86400) / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  if (d > 0) return `${d}d ${h}h ${m}m`;
  if (h > 0) return `${h}h ${m}m`;
  return `${m}m`;
}

function formatBytes(bytes: number): string {
  if (bytes === 0) return "0 B";
  const units = ["B", "KB", "MB", "GB"];
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
  return `${(bytes / 1024 ** i).toFixed(i > 0 ? 1 : 0)} ${units[i]}`;
}

const salesColumns = [
  { key: "invoice_id", label: "Invoice", mono: true },
  { key: "price", label: "Price (sat)" },
  {
    key: "paid",
    label: "Status",
    render: (row: SaleRecord) => (
      <Badge variant={row.paid ? "success" : "warn"}>
        {row.paid ? "Paid" : "Pending"}
      </Badge>
    ),
  },
];

function DashboardHome() {
  const { data: status } = usePolling<StatusResponse>("/_bitfs/dashboard/status");
  const { data: storage } = usePolling<StorageResponse>("/_bitfs/dashboard/storage");
  const { data: sales } = usePolling<SaleRecord[]>("/_bitfs/sales", 10000);

  return (
    <div>
      <h1 className="text-2xl font-bold">Dashboard</h1>
      <p className="mt-1 text-sm text-text-secondary">Node overview and status</p>
      <div className="mt-6 grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <StatCard label="Uptime" value={status ? formatUptime(status.uptime_seconds) : "—"} icon={Clock} />
        <StatCard label="Files" value={storage ? storage.file_count : "—"} icon={HardDrive} />
        <StatCard label="Storage" value={storage ? formatBytes(storage.total_size_bytes) : "—"} icon={HardDrive} />
        <StatCard label="Version" value={status?.version ?? "—"} icon={Link} mono />
      </div>
      <h2 className="mt-8 text-lg font-semibold">Recent Sales</h2>
      <div className="mt-3">
        <DataTable columns={salesColumns} data={sales ?? []} emptyMessage="No sales yet" />
      </div>
    </div>
  );
}

export default DashboardHome;
