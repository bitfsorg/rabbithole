import { Globe, Shield, Radio } from "lucide-react";
import { StatCard } from "@/components/StatCard";
import { Badge } from "@/components/Badge";
import { usePolling } from "@/hooks/usePolling";
import type { NetworkResponse } from "@/lib/api";

function Network() {
  const { data, error, loading } = usePolling<NetworkResponse>("/_bitfs/dashboard/network");

  return (
    <div>
      <h1 className="text-2xl font-bold">Network</h1>
      <p className="mt-1 text-sm text-text-secondary">Blockchain connectivity and SPV status</p>
      {error && <p className="mt-4 text-sm text-error">Failed to load: {error}</p>}
      <div className="mt-6 grid grid-cols-1 gap-4 sm:grid-cols-3">
        <StatCard label="Network" value={data ? (data.mainnet ? "Mainnet" : "Testnet / Regtest") : loading ? "..." : "—"} icon={Globe} />
        <StatCard label="SPV" value={data ? (data.spv_enabled ? "Enabled" : "Disabled") : loading ? "..." : "—"} icon={Shield} />
        <StatCard label="Blockchain Service" value={data ? "Connected" : loading ? "..." : "—"} icon={Radio} />
      </div>
      <div className="mt-6 rounded-lg border border-border bg-bg-card p-4">
        <h3 className="text-sm font-medium text-text-secondary">Connection Status</h3>
        <div className="mt-3 flex items-center gap-2">
          <span className={`h-2 w-2 rounded-full ${data ? "bg-success" : "bg-error"}`} />
          <span className="text-sm">{data ? "Daemon responding" : error ? "Connection error" : "Loading..."}</span>
        </div>
        {data && (
          <div className="mt-2 flex items-center gap-2">
            <Badge variant={data.spv_enabled ? "success" : "warn"}>SPV {data.spv_enabled ? "ON" : "OFF"}</Badge>
          </div>
        )}
      </div>
    </div>
  );
}

export default Network;
