function env(key: string, fallback: string): string {
  return process.env[key] || fallback;
}

function envInt(key: string, fallback: number): number {
  const v = process.env[key];
  return v ? parseInt(v, 10) : fallback;
}

function envBool(key: string, fallback: boolean): boolean {
  const v = process.env[key];
  if (v === undefined) return fallback;
  return ["1", "true", "yes", "on"].includes(v.trim().toLowerCase());
}

export const config = {
  redisUrl: env("REDIS_URL", ""),
  redisHost: env("REDIS_HOST", "localhost"),
  redisPort: envInt("REDIS_PORT", 6379),
  redisPrefix: env("REDIS_PREFIX", "sales"),
  redisTlsInsecure: envBool("REDIS_TLS_INSECURE", true),

  concurrency: envInt("CONCURRENCY", 2),
  jobTimeoutMs: envInt("JOB_TIMEOUT_MS", 480_000), // 8 min
  batchSize: envInt("BATCH_SIZE", 20),

  requestDelayMinMs: envInt("REQUEST_DELAY_MIN_MS", 2000),
  requestDelayMaxMs: envInt("REQUEST_DELAY_MAX_MS", 8000),
} as const;
