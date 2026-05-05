import { Redis } from "ioredis";
import { config } from "../config.js";
import type { ScrapeJob } from "../types/job.js";
import type { BaseScraper } from "../scrapers/base.js";
import { listenForKill } from "./kill-listener.js";
import { log } from "../utils/logger.js";

async function readJobFromStdin(): Promise<ScrapeJob> {
  const chunks: Buffer[] = [];
  for await (const chunk of process.stdin) {
    chunks.push(Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk));
  }
  const raw = Buffer.concat(chunks).toString("utf8").trim();
  if (!raw) {
    throw new Error("missing job payload on stdin");
  }
  return JSON.parse(raw) as ScrapeJob;
}

async function loadScraper(source: string): Promise<BaseScraper | null> {
  switch (source) {
    case "google_maps": {
      const m = await import("../scrapers/google-maps.js");
      return new m.GoogleMapsScraper();
    }
    case "yelp": {
      const m = await import("../scrapers/yelp.js");
      return new m.YelpScraper();
    }
    case "yellow_pages": {
      const m = await import("../scrapers/yellow-pages.js");
      return new m.YellowPagesScraper();
    }
    case "google_dorks": {
      const m = await import("../scrapers/google-dorks.js");
      return new m.GoogleDorksScraper();
    }
    case "new_domains": {
      const m = await import("../scrapers/new-domains.js");
      return new m.NewDomainsScraper();
    }
    case "reddit": {
      const m = await import("../scrapers/reddit.js");
      return new m.RedditScraper();
    }
    case "custom_urls": {
      const m = await import("../scrapers/custom-urls.js");
      return new m.CustomUrlsScraper();
    }
    case "web_crawler": {
      const m = await import("../crawler/crawler.js");
      return new m.WebCrawlerScraper();
    }
    case "linkedin": {
      const m = await import("../scrapers/linkedin.js");
      return new m.LinkedInScraper();
    }
    case "justdial": {
      const m = await import("../scrapers/justdial.js");
      return new m.JustDialScraper();
    }
    default:
      return null;
  }
}

function shouldUseTls(redisUrl: string): boolean {
  if (!redisUrl) return config.redisUseTls;
  return redisUrl.startsWith("rediss://") || config.redisUseTls;
}

async function main(): Promise<void> {
  const job = await readJobFromStdin();

  const useTls = shouldUseTls(config.redisUrl);
  const tlsOptions = useTls ? { rejectUnauthorized: !config.redisTlsInsecure } : undefined;
  const redis = config.redisUrl
    ? new Redis(config.redisUrl, { maxRetriesPerRequest: null, tls: tlsOptions })
    : new Redis({
        host: config.redisHost,
        port: config.redisPort,
        maxRetriesPerRequest: null,
        tls: tlsOptions,
      });

  const pushStatus = async (status: string, error?: string): Promise<void> => {
    const payload = JSON.stringify({ job_id: job.job_id, status, ...(error ? { error } : {}) });
    await redis.rpush(`${config.redisPrefix}:job_status`, payload);
  };

  const scraper = await loadScraper(job.source);
  if (!scraper) {
    await pushStatus("failed", `unknown source: ${job.source}`);
    redis.disconnect();
    process.exit(2);
  }

  const { controller, cleanup } = listenForKill(
    config.redisHost,
    config.redisPort,
    job.job_id,
    config.redisUrl || undefined,
  );

  const timeout = setTimeout(() => {
    log.warn("job timed out", { job_id: job.job_id, timeout_ms: config.jobTimeoutMs });
    controller.abort();
  }, config.jobTimeoutMs);

  try {
    await pushStatus("in_progress");

    for await (const rawBatch of scraper.scrape(job, controller.signal)) {
      if (controller.signal.aborted) break;

      const batch = job.drop_no_contact ? rawBatch.filter((l) => l.phone || l.email) : rawBatch;
      if (batch.length === 0) continue;

      const payload = JSON.stringify({ job_id: job.job_id, leads: batch });
      await redis.rpush(`${config.redisPrefix}:lead_batches`, payload);
      log.info("leads pushed to Redis", { job_id: job.job_id, count: batch.length });
    }

    if (controller.signal.aborted) {
      await pushStatus("timeout");
    } else {
      await pushStatus("completed");
    }
  } catch (err: any) {
    const msg = err?.message || String(err);
    log.error("job failed", { job_id: job.job_id, error: msg });
    await pushStatus("failed", msg).catch(() => {});
    process.exitCode = 1;
  } finally {
    clearTimeout(timeout);
    cleanup();
    redis.disconnect();
  }
}

main().catch((err) => {
  log.error("job worker fatal error", { error: String(err) });
  process.exit(1);
});
