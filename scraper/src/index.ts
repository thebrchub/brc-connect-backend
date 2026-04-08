import { Redis } from "ioredis";
import { config } from "./config.js";
import { GoClient } from "./api/go-client.js";
import { Runner } from "./worker/runner.js";
import { log } from "./utils/logger.js";

async function main(): Promise<void> {
  log.info("starting scraper service");

  // 1. Connect Redis
  const redis = config.redisUrl
    ? new Redis(config.redisUrl, { maxRetriesPerRequest: null })
    : new Redis({
        host: config.redisHost,
        port: config.redisPort,
        maxRetriesPerRequest: null,
      });

  redis.on("error", (err: Error) => {
    log.error("redis connection error", { error: String(err) });
  });

  log.info("redis connected", {
    host: config.redisHost,
    port: config.redisPort,
  });

  // 2. Authenticate with Go API
  const api = new GoClient();
  await api.login();

  // 3. Start worker loop
  const runner = new Runner(redis, api);

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
