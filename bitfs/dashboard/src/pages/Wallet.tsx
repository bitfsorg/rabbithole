import { Wallet as WalletIcon, Key, Copy } from "lucide-react";
import { StatCard } from "@/components/StatCard";
import { usePolling } from "@/hooks/usePolling";
import type { WalletResponse } from "@/lib/api";
import { useState } from "react";

function Wallet() {
  const { data, error, loading } = usePolling<WalletResponse>("/_bitfs/dashboard/wallet");
  const [copied, setCopied] = useState(false);

  const copyPubkey = async () => {
    if (data?.pubkey) {
      await navigator.clipboard.writeText(data.pubkey);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    }
  };

  return (
    <div>
      <h1 className="text-2xl font-bold">Wallet</h1>
      <p className="mt-1 text-sm text-text-secondary">HD wallet information</p>
      {error && <p className="mt-4 text-sm text-error">Failed to load: {error}</p>}
      <div className="mt-6 grid grid-cols-1 gap-4 sm:grid-cols-2">
        <StatCard label="Status" value={data ? (data.available ? "Active" : "Not configured") : loading ? "..." : "—"} icon={WalletIcon} />
        <StatCard label="Derivation" value="m/44'/236'/0'" icon={Key} mono />
      </div>
      {data?.pubkey && (
        <div className="mt-6 rounded-lg border border-border bg-bg-card p-4">
          <div className="flex items-center justify-between">
            <span className="text-sm text-text-secondary">Vault Public Key</span>
            <button onClick={copyPubkey} className="flex items-center gap-1 rounded px-2 py-1 text-xs text-text-secondary hover:bg-bg-card-hover hover:text-accent">
              <Copy className="h-3 w-3" />
              {copied ? "Copied" : "Copy"}
            </button>
          </div>
          <p className="mt-2 break-all font-mono text-sm">{data.pubkey}</p>
        </div>
      )}
    </div>
  );
}

export default Wallet;
