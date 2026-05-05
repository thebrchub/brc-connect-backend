import { Redis } from "ioredis";
import { spawn } from "node:child_process";
import { config } from "../config.js";
import { ScrapeJob } from "../types/job.js";
import { log } from "../utils/logger.js";

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

  private async processJob(job: ScrapeJob, workerId: number): Promise<void> {
    const workerScript = new URL("./job-worker.js", import.meta.url);

    await new Promise<void>((resolve) => {
      const child = spawn(process.execPath, [workerScript.pathname], {
        stdio: ["pipe", "inherit", "inherit"],
        env: process.env,
      });

      child.once("error", (err) => {
        log.error("job worker spawn failed", {
          worker: workerId,
          job_id: job.job_id,
          error: String(err),
        });
        resolve();
      });

      child.once("exit", (code, signal) => {
        if (code !== 0) {
          log.error("job worker exited with error", {
            worker: workerId,
            job_id: job.job_id,
            code: code ?? -1,
            signal: signal ?? "",
          });
        }
        resolve();
      });

      child.stdin.write(JSON.stringify(job));
      child.stdin.end();
    });
  }
}

function sleep(ms: number): Promise<void> {
  return new Promise((r) => setTimeout(r, ms));
}
