import { HardDrive, FolderOpen, Database } from "lucide-react";
import { StatCard } from "@/components/StatCard";
import { usePolling } from "@/hooks/usePolling";
import type { StorageResponse } from "@/lib/api";

function formatBytes(bytes: number): string {
  if (bytes === 0) return "0 B";
  const units = ["B", "KB", "MB", "GB"];
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
  return `${(bytes / 1024 ** i).toFixed(i > 0 ? 1 : 0)} ${units[i]}`;
}

function Storage() {
  const { data, error, loading } = usePolling<StorageResponse>("/_bitfs/dashboard/storage");

  return (
    <div>
      <h1 className="text-2xl font-bold">Storage</h1>
      <p className="mt-1 text-sm text-text-secondary">Content-addressed file store</p>
      {error && <p className="mt-4 text-sm text-error">Failed to load: {error}</p>}
      <div className="mt-6 grid grid-cols-1 gap-4 sm:grid-cols-3">
        <StatCard label="Files" value={data ? data.file_count : loading ? "..." : "—"} icon={HardDrive} />
        <StatCard label="Total Size" value={data ? formatBytes(data.total_size_bytes) : loading ? "..." : "—"} icon={Database} />
        <StatCard label="Storage Path" value={data?.storage_path ?? "—"} icon={FolderOpen} mono />
      </div>
    </div>
  );
}

export default Storage;
