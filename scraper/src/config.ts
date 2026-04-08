function env(key: string, fallback: string): string {
  return process.env[key] || fallback;
}

function envInt(key: string, fallback: number): number {
  const v = process.env[key];
  return v ? parseInt(v, 10) : fallback;
}

export const config = {
  apiBaseUrl: env("API_BASE_URL", "http://localhost:8080"),
  serviceUser: env("SERVICE_USER", "scraper"),
  servicePass: env("SERVICE_PASS", ""),

  redisUrl: env("REDIS_URL", ""),
  redisHost: env("REDIS_HOST", "localhost"),
  redisPort: envInt("REDIS_PORT", 6379),

  concurrency: envInt("CONCURRENCY", 2),
  jobTimeoutMs: envInt("JOB_TIMEOUT_MS", 480_000), // 8 min
  batchSize: envInt("BATCH_SIZE", 20),

  requestDelayMinMs: envInt("REQUEST_DELAY_MIN_MS", 2000),
  requestDelayMaxMs: envInt("REQUEST_DELAY_MAX_MS", 8000),
} as const;
