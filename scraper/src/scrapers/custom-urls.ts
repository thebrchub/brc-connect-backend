import axios from "axios";
import * as cheerio from "cheerio";
import { BaseScraper } from "./base.js";
import { ScrapeJob } from "../types/job.js";
import { RawLead } from "../types/lead.js";
import { config } from "../config.js";
import { randomUserAgent } from "../anti-ban/user-agents.js";
import { randomDelay } from "../anti-ban/delays.js";
import { extractEmails, extractPhones } from "../extractors/contact.js";
import { detectTechStack, hasSSL } from "../extractors/tech-stack.js";
import { Frontier } from "../crawler/frontier.js";
import { sortByPriority } from "../crawler/link-filter.js";
import { log } from "../utils/logger.js";

const MAX_PAGES_PER_SITE = 10;

/**
 * Custom URL list scraper — user provides a list of URLs to crawl.
 * For each URL, crawls the site (BFS, prioritizing /contact /about)
 * and extracts contact info.
 */
export class CustomUrlsScraper extends BaseScraper {
  readonly source = "custom_urls";

  async *scrape(
    job: ScrapeJob,
    signal: AbortSignal
  ): AsyncGenerator<RawLead[], void, unknown> {
    const urls = job.urls ?? [];
    if (urls.length === 0) {
      log.error("custom_urls job has no URLs", { job_id: job.job_id });
      return;
    }

    let batch: RawLead[] = [];

    for (const url of urls) {
      if (signal.aborted) break;

      const lead = await this.crawlSite(url, job, signal);
      if (lead) {
        batch.push(lead);
        if (batch.length >= config.batchSize) {
          yield batch;
          batch = [];
        }
      }

      await randomDelay();
    }

    if (batch.length > 0) {
      yield batch;
    }
  }

  private async crawlSite(
    startUrl: string,
    job: ScrapeJob,
    signal: AbortSignal
  ): Promise<RawLead | null> {
    const domain = this.extractDomain(startUrl);
    if (!domain) return null;

    const frontier = new Frontier(2, MAX_PAGES_PER_SITE);
    frontier.add(startUrl, 0);

    let businessName: string | null = null;
    const allEmails = new Set<string>();
    const allPhones = new Set<string>();
    let pagesCrawled = 0;

    while (!frontier.isEmpty() && !signal.aborted && pagesCrawled < MAX_PAGES_PER_SITE) {
      const entry = frontier.next();
      if (!entry) break;

      try {
        const { data } = await axios.get<string>(entry.url, {
          timeout: 10_000,
          headers: { "User-Agent": randomUserAgent() },
          signal,
          maxRedirects: 3,
          responseType: "text",
        });

        pagesCrawled++;

        const $ = cheerio.load(data);
        const text = $.text();

        for (const email of extractEmails(text)) allEmails.add(email);
        for (const phone of extractPhones(text)) allPhones.add(phone);

        if (!businessName) {
          businessName =
            $("title").text().trim()
            || $('meta[property="og:site_name"]').attr("content")?.trim()
            || domain;
        }

        // Discover links for BFS
        const discoveredLinks: string[] = [];
        $("a[href]").each((_, el) => {
          const href = $(el).attr("href");
          if (!href) return;
          try {
            discoveredLinks.push(new URL(href, entry.url).toString());
          } catch { /* invalid URL */ }
        });

        const sorted = sortByPriority(discoveredLinks, domain);
        for (const link of sorted) {
          frontier.add(link, entry.depth + 1);
        }

        await randomDelay();
      } catch {
        // page fetch failed
      }
    }

    if (allEmails.size === 0 && allPhones.size === 0) return null;

    const techStack = await detectTechStack(startUrl, signal).catch(() => null);

    return {
      business_name: businessName || domain,
      phone: [...allPhones][0] ?? null,
      email: [...allEmails][0] ?? null,
      website_url: startUrl,
      address: null,
      city: job.city,
      country: "US",
      category: job.category,
      source: this.source,
      source_url: startUrl,
      tech_stack: techStack,
      has_ssl: hasSSL(startUrl),
      is_mobile_friendly: null,
    };
  }

  private extractDomain(url: string): string | null {
    try {
      return new URL(url).hostname.replace(/^www\./, "");
    } catch {
      return null;
    }
  }
}
