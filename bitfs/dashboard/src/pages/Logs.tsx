import { Badge } from "@/components/Badge";
import { DataTable } from "@/components/DataTable";
import { usePolling } from "@/hooks/usePolling";
import type { LogEntry, LogsResponse } from "@/lib/api";
import { useState } from "react";

const levelVariant: Record<string, string> = {
  info: "info",
  warn: "warn",
  error: "error",
};

function Logs() {
  const [level, setLevel] = useState("");
  const { data, error } = usePolling<LogsResponse>(
    `/_bitfs/dashboard/logs?limit=100${level ? `&level=${level}` : ""}`,
  );

  const columns = [
    {
      key: "timestamp",
      label: "Time",
      mono: true,
      render: (row: LogEntry) => {
        const d = new Date(row.timestamp);
        return d.toLocaleTimeString();
      },
    },
    {
      key: "level",
      label: "Level",
      render: (row: LogEntry) => (
        <Badge variant={levelVariant[row.level] ?? "info"}>
          {row.level.toUpperCase()}
        </Badge>
      ),
    },
    { key: "message", label: "Message", mono: true },
  ];

  return (
    <div>
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold">Logs</h1>
          <p className="mt-1 text-sm text-text-secondary">Daemon log entries (auto-refresh 5s)</p>
        </div>
        <select
          value={level}
          onChange={(e) => setLevel(e.target.value)}
          className="rounded border border-border bg-bg-card px-3 py-1.5 text-sm text-text-primary"
        >
          <option value="">All levels</option>
          <option value="info">Info</option>
          <option value="warn">Warn</option>
          <option value="error">Error</option>
        </select>
      </div>
      {error && <p className="mt-4 text-sm text-error">Failed to load: {error}</p>}
      <div className="mt-4">
        <DataTable columns={columns} data={data?.entries ?? []} emptyMessage="No log entries" />
      </div>
    </div>
  );
}

export default Logs;
