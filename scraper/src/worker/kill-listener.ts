import { Redis } from "ioredis";
import { config } from "../config.js";
import { log } from "../utils/logger.js";

/**
 * Subscribes to Redis kill channel for a job.
 * Returns an AbortController — signal is aborted when kill is received.
 */
export function listenForKill(
  redisHost: string,
  redisPort: number,
  jobId: string,
  redisUrl?: string,
): { controller: AbortController; cleanup: () => void } {
  const controller = new AbortController();
  const channel = `job_kill`;
  const tlsOptions = config.redisTlsInsecure ? { rejectUnauthorized: false } : undefined;
  const sub = redisUrl
    ? new Redis(redisUrl, { maxRetriesPerRequest: null, tls: tlsOptions })
    : new Redis({ host: redisHost, port: redisPort, maxRetriesPerRequest: null, tls: tlsOptions });

  let cleaned = false;

  const handler = (ch: string, message: string) => {
    if (ch === channel && message === jobId) {
      log.warn("kill signal received", { job_id: jobId });
      controller.abort();
    }
  };

  sub.subscribe(channel).catch((err: Error) => {
    log.error("failed to subscribe to kill channel", { error: String(err) });
  });
  sub.on("message", handler);

  function cleanup() {
    if (cleaned) return;
    cleaned = true;
    sub.off("message", handler);
    sub.unsubscribe(channel).catch(() => {});
    sub.disconnect();
  }

  return { controller, cleanup };
}
