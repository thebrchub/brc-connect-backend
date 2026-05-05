import { Redis } from "ioredis";
import { config } from "../config.js";
import { listenForKill } from "./kill-listener.js";
import { ScrapeJob } from "../types/job.js";
import { BaseScraper } from "../scrapers/base.js";
import { GoogleMapsScraper } from "../scrapers/google-maps.js";
import { YelpScraper } from "../scrapers/yelp.js";
import { YellowPagesScraper } from "../scrapers/yellow-pages.js";
import { GoogleDorksScraper } from "../scrapers/google-dorks.js";
import { NewDomainsScraper } from "../scrapers/new-domains.js";
import { RedditScraper } from "../scrapers/reddit.js";
import { CustomUrlsScraper } from "../scrapers/custom-urls.js";
import { WebCrawlerScraper } from "../crawler/crawler.js";
import { LinkedInScraper } from "../scrapers/linkedin.js";
import { JustDialScraper } from "../scrapers/justdial.js";
import { log } from "../utils/logger.js";

/** Scraper dispatch map — add new sources here. */
const scraperMap: Record<string, BaseScraper> = {
  google_maps: new GoogleMapsScraper(),
  yelp: new YelpScraper(),
  yellow_pages: new YellowPagesScraper(),
  google_dorks: new GoogleDorksScraper(),
  new_domains: new NewDomainsScraper(),
  reddit: new RedditScraper(),
  custom_urls: new CustomUrlsScraper(),
  web_crawler: new WebCrawlerScraper(),
  linkedin: new LinkedInScraper(),
  justdial: new JustDialScraper(),
};

export class Runner {
  private redis: Redis;
  private running = true;

  constructor(redis: Redis) {
    this.redis = redis;
  }

  /** Start the BLPOP worker loop. */
  async start(): Promise<void> {
    log.info("worker loop started", { concurrency: config.concurrency });

    const workers = Array.from({ length: config.concurrency }, (_, i) =>
      this.workerLoop(i)
    );
    await Promise.all(workers);
  }

  stop(): void {
    this.running = false;
  }

  private async workerLoop(workerId: number): Promise<void> {
    while (this.running) {
      try {
        // BLPOP blocks until a job is available (30s timeout then re-loop)
        const result = await this.redis.blpop(`${config.redisPrefix}:scrape_queue`, 30);
        if (!result) continue; // timeout, re-loop

        const [, payload] = result;
        const job: ScrapeJob = JSON.parse(payload);

        await this.processJob(job, workerId);
      } catch (err) {
        if (!this.running) break;
        log.error("worker loop error", { worker: workerId, error: String(err) });
        // Brief pause before retrying to avoid tight error loops
        await sleep(2000);
      }
    }
  }

  /** Push a status update to Redis for Go to pick up. */
  private async pushStatus(jobId: string, status: string, error?: string): Promise<void> {
    const payload = JSON.stringify({ job_id: jobId, status, ...(error ? { error } : {}) });
    await this.redis.rpush(`${config.redisPrefix}:job_status`, payload);
  }

  private async processJob(job: ScrapeJob, workerId: number): Promise<void> {
    const scraper = scraperMap[job.source];
    if (!scraper) {
      log.error("unknown source, skipping", { source: job.source, job_id: job.job_id });
      await this.pushStatus(job.job_id, "failed", `unknown source: ${job.source}`);
      return;
    }

    // Set up kill listener + timeout
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
      // Notify Go via Redis that job is in progress
      await this.pushStatus(job.job_id, "in_progress");

      // Run the scraper — it yields batches of leads
      for await (const rawBatch of scraper.scrape(job, controller.signal)) {
        if (controller.signal.aborted) break;

        // Drop leads without both phone and email if configured
        const batch = job.drop_no_contact
          ? rawBatch.filter((l) => l.phone || l.email)
          : rawBatch;
        if (batch.length === 0) continue;

        // Push leads to Redis queue for Go to process
        const payload = JSON.stringify({ job_id: job.job_id, leads: batch });
        await this.redis.rpush(`${config.redisPrefix}:lead_batches`, payload);
        log.info("leads pushed to Redis", { job_id: job.job_id, count: batch.length });
      }

      if (controller.signal.aborted) {
        await this.pushStatus(job.job_id, "timeout");
      } else {
        await this.pushStatus(job.job_id, "completed");
      }
    } catch (err: any) {
      const msg = err?.message || String(err);
      log.error("job failed", { job_id: job.job_id, error: msg });
      await this.pushStatus(job.job_id, "failed", msg).catch(() => {});
    } finally {
      clearTimeout(timeout);
      cleanup();
    }
  }
}

function sleep(ms: number): Promise<void> {
  return new Promise((r) => setTimeout(r, ms));
}
