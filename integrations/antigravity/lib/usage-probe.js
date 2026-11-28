import { AgyAcpClient } from "./acp.js";
import { readNativeQuota } from "./usage.js";

export async function readAgyUsage(bin) {
  const client = new AgyAcpClient({ bin, initTimeoutMs: 12000 });
  try {
    return await readNativeQuota(client);
  } finally {
    await client.stop();
  }
}
