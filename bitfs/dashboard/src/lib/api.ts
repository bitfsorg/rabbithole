/** Base URL for the BitFS daemon API. */
export const API_BASE = "/_api";

/** Fetch daemon status (version, uptime, etc.). */
// TODO: implement daemon HTTP client
export async function getStatus(): Promise<unknown> {
  throw new Error("not implemented");
}

/** Fetch storage statistics (total files, disk usage, etc.). */
// TODO: implement daemon HTTP client
export async function getStorageStats(): Promise<unknown> {
  throw new Error("not implemented");
}

/** Fetch wallet information (address, balance, etc.). */
// TODO: implement daemon HTTP client
export async function getWalletInfo(): Promise<unknown> {
  throw new Error("not implemented");
}

/** Fetch connected network peers. */
// TODO: implement daemon HTTP client
export async function getNetworkPeers(): Promise<unknown> {
  throw new Error("not implemented");
}
