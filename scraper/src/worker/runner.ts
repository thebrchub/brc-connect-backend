import { Redis } from "ioredis";
import { config } from "../config.js";
import { GoClient } from "../api/go-client.js";
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
};

export class Runner {
  private redis: Redis;
  private api: GoClient;
  private running = true;

  constructor(redis: Redis, api: GoClient) {
    this.redis = redis;
    this.api = api;
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
        const result = await this.redis.blpop("scrape_queue", 30);
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

  private async processJob(job: ScrapeJob, workerId: number): Promise<void> {
    const scraper = scraperMap[job.source];
    if (!scraper) {
      log.error("unknown source, skipping", { source: job.source, job_id: job.job_id });
      await this.api.updateJobStatus(job.job_id, "failed", 0, `unknown source: ${job.source}`);
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

    let totalLeads = 0;

    try {
      // Notify Go API that job is in progress
      await this.api.updateJobStatus(job.job_id, "in_progress", 0);

      // Run the scraper — it yields batches of leads
      for await (const batch of scraper.scrape(job, controller.signal)) {
        if (controller.signal.aborted) break;

        // POST batch to Go API
        const result = await this.api.submitLeads(job.job_id, batch);
        totalLeads += result.inserted + result.merged;
      }

      if (controller.signal.aborted) {
        await this.api.updateJobStatus(job.job_id, "timeout", totalLeads);
      } else {
        await this.api.updateJobStatus(job.job_id, "completed", totalLeads);
      }
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      log.error("job failed", { job_id: job.job_id, error: msg });
      await this.api
        .updateJobStatus(job.job_id, "failed", totalLeads, msg)
        .catch(() => {});
    } finally {
      clearTimeout(timeout);
      cleanup();
    }
  }
}

function sleep(ms: number): Promise<void> {
  return new Promise((r) => setTimeout(r, ms));
}
