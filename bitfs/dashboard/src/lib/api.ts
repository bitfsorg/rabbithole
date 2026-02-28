export const API_BASE = "/_bitfs/dashboard";

export interface StatusResponse {
  version: string;
  uptime_seconds: number;
  listen_addr: string;
  started_at: string;
  mainnet: boolean;
  vault_pnode?: string;
}

export interface StorageResponse {
  file_count: number;
  total_size_bytes: number;
  storage_path: string;
}

export interface WalletResponse {
  available: boolean;
  pubkey?: string;
}

export interface NetworkResponse {
  mainnet: boolean;
  spv_enabled: boolean;
}

export interface LogEntry {
  timestamp: string;
  level: string;
  message: string;
}

export interface LogsResponse {
  entries: LogEntry[];
}

export interface SaleRecord {
  invoice_id: string;
  price: number;
  key_hash: string;
  timestamp: number;
  paid: boolean;
}
