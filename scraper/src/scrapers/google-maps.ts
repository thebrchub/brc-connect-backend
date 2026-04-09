import puppeteer, { Browser, Page } from "puppeteer";
import { BaseScraper } from "./base.js";
import { ScrapeJob } from "../types/job.js";
import { RawLead } from "../types/lead.js";
import { config } from "../config.js";
import { randomUserAgent } from "../anti-ban/user-agents.js";
import { randomDelay } from "../anti-ban/delays.js";
import { extractEmails, extractPhones } from "../extractors/contact.js";
import { validatePhone } from "../extractors/phone.js";
import { detectTechStack, hasSSL } from "../extractors/tech-stack.js";
import { log } from "../utils/logger.js";

const MAPS_URL = "https://www.google.com/maps/search/";

export class GoogleMapsScraper extends BaseScraper {
  readonly source = "google_maps";

  async *scrape(
    job: ScrapeJob,
    signal: AbortSignal
  ): AsyncGenerator<RawLead[], void, unknown> {
    let browser: Browser | null = null;

    try {
      browser = await puppeteer.launch({
        headless: true,
        executablePath: process.env.PUPPETEER_EXECUTABLE_PATH || undefined,
        args: [
          "--no-sandbox",
          "--disable-setuid-sandbox",
          "--disable-dev-shm-usage",
          "--disable-gpu",
          "--disable-crashpad",
          "--no-zygote",
          "--single-process",
        ],
      });

      const page = await browser.newPage();
      await page.setUserAgent(randomUserAgent());
      await page.setViewport({ width: 1280, height: 900 });

      const query = encodeURIComponent(`${job.category} ${job.city}`);
      await page.goto(`${MAPS_URL}${query}`, {
        waitUntil: "networkidle2",
        timeout: 30_000,
      });

      // Wait for results panel to load
      await page.waitForSelector('div[role="feed"]', { timeout: 15_000 }).catch(() => {});

      let batch: RawLead[] = [];
      const seenNames = new Set<string>();
      let scrollAttempts = 0;
      const maxScrollAttempts = 30;

      while (scrollAttempts < maxScrollAttempts && !signal.aborted) {
        // Extract visible results
        const results = await this.extractResults(page, job);

        for (const lead of results) {
          if (signal.aborted) break;
          if (seenNames.has(lead.business_name.toLowerCase())) continue;
          seenNames.add(lead.business_name.toLowerCase());

          // Enrich: visit website for contact info + tech stack
          if (lead.website_url) {
            const enriched = await this.enrichLead(lead, signal);
            batch.push(enriched);
          } else {
            batch.push(lead);
          }

          // Yield batch when full
          if (batch.length >= config.batchSize) {
            yield batch;
            batch = [];
          }
        }

        // Scroll the results panel
        const scrolled = await this.scrollResults(page);
        if (!scrolled) break; // no more results
        scrollAttempts++;

        await randomDelay();
      }

      // Yield remaining — always yield partial results even on abort
      if (batch.length > 0) {
        yield batch;
      }
    } catch (err) {
      if (!signal.aborted) {
        log.error("google maps scraper error", {
          job_id: job.job_id,
          error: err instanceof Error ? err.message : String(err),
        });
        throw err;
      }
    } finally {
      if (browser) {
        await browser.close().catch(() => {});
      }
    }
  }

  private async extractResults(
    page: Page,
    job: ScrapeJob
  ): Promise<RawLead[]> {
    return page.evaluate((source: string, city: string, category: string) => {
      const items = document.querySelectorAll('div[role="feed"] > div > div > a');
      const results: any[] = [];

      items.forEach((item) => {
        const nameEl = item.getAttribute("aria-label");
        if (!nameEl) return;

        // The listing URL is the href of the link itself
        const sourceUrl = (item as HTMLAnchorElement).href ?? null;

        // Get the parent container for more info
        const container = item.closest('div[jsaction]');
        const text = container?.textContent ?? "";

        // Extract phone from text
        const phoneMatch = text.match(
          /(?:\+?\d{1,3}[-.\s]?)?\(?\d{2,5}\)?[-.\s]?\d{3,5}[-.\s]?\d{3,5}[-.\s]?\d{0,4}/
        );

        // Extract website URL
        const websiteLink = container?.querySelector('a[data-value="Website"]');
        const websiteUrl = websiteLink?.getAttribute("href") ?? null;

        // Extract address — typically in a span with specific class
        const spans = container?.querySelectorAll("span") ?? [];
        let address: string | null = null;
        for (const span of spans) {
          const t = span.textContent?.trim() ?? "";
          // Address usually contains numbers and commas
          if (/\d+.*,/.test(t) && t.length > 10 && t.length < 200) {
            address = t;
            break;
          }
        }

        results.push({
          business_name: nameEl,
          phone: phoneMatch?.[0] ?? null,
          email: null, // emails extracted from website
          website_url: websiteUrl,
          address,
          city,
          country: null, // detected by Go API from city
          category,
          source,
          source_url: sourceUrl,
          tech_stack: null,
          has_ssl: websiteUrl ? websiteUrl.startsWith("https") : null,
          is_mobile_friendly: null,
        });
      });

      return results;
    }, job.source, job.city, job.category) as Promise<RawLead[]>;
  }

  private async enrichLead(
    lead: RawLead,
    signal: AbortSignal
  ): Promise<RawLead> {
    if (!lead.website_url || signal.aborted) return lead;

    try {
      const [techStack] = await Promise.all([
        detectTechStack(lead.website_url, signal),
      ]);

      // Fetch website HTML for contact extraction
      const axios = (await import("axios")).default;
      const { data } = await axios.get<string>(lead.website_url, {
        timeout: 8_000,
        signal,
        maxRedirects: 3,
        responseType: "text",
        headers: { "User-Agent": randomUserAgent() },
      });

      if (typeof data === "string") {
        const emails = extractEmails(data);
        const phones = extractPhones(data);

        if (!lead.email && emails.length > 0) {
          lead.email = emails[0];
        }
        if (!lead.phone && phones.length > 0) {
          const validated = validatePhone(phones[0]);
          if (validated?.valid) {
            lead.phone = validated.e164;
          }
        }
      }

      if (techStack) {
        lead.tech_stack = techStack;
      }
      lead.has_ssl = hasSSL(lead.website_url);
    } catch {
      // enrichment failure is not fatal — proceed with what we have
    }

    return lead;
  }

  private async scrollResults(page: Page): Promise<boolean> {
    return page.evaluate(() => {
      const feed = document.querySelector('div[role="feed"]');
      if (!feed) return false;

      const prevHeight = feed.scrollHeight;
      feed.scrollTo(0, feed.scrollHeight);

      return new Promise<boolean>((resolve) => {
        setTimeout(() => {
          resolve(feed.scrollHeight > prevHeight);
        }, 2000);
      });
    });
  }
}
