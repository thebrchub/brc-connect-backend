import { Redis } from "ioredis";
import { config } from "./config.js";
import { Runner } from "./worker/runner.js";
import { log } from "./utils/logger.js";

async function main(): Promise<void> {
  log.info("starting scraper service");

  // 1. Connect Redis
  const tlsOptions = config.redisTlsInsecure ? { rejectUnauthorized: false } : undefined;
  const redis = config.redisUrl
    ? new Redis(config.redisUrl, { maxRetriesPerRequest: null, tls: tlsOptions })
    : new Redis({
        host: config.redisHost,
        port: config.redisPort,
        maxRetriesPerRequest: null,
        tls: tlsOptions,
      });

  redis.on("error", (err: Error) => {
    log.error("redis connection error", { error: String(err) });
  });

  log.info("redis connected", {
    target: config.redisUrl ? config.redisUrl.replace(/\/\/.*@/, "//***@") : `${config.redisHost}:${config.redisPort}`,
  });

  // 2. Start worker loop (no HTTP — all communication via Redis)
  const runner = new Runner(redis);

  // Graceful shutdown
  const shutdown = async (signal: string) => {
    log.info(`${signal} received, shutting down`);
    runner.stop();
    redis.disconnect();
    process.exit(0);
  };

  process.on("SIGINT", () => shutdown("SIGINT"));
  process.on("SIGTERM", () => shutdown("SIGTERM"));

  await runner.start();
}

main().catch((err) => {
  log.error("fatal startup error", { error: String(err) });
  process.exit(1);
});
