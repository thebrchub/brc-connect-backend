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
import { log } from "../utils/logger.js";

const MAX_PAGES = 5;
const RESULTS_PER_PAGE = 10;

/**
 * Google Dork scraper — searches Google with dork queries
 * like "built with WordPress" + "contact us" + city.
 * Extracts URLs from SERP, visits each for contacts.
 */
export class GoogleDorksScraper extends BaseScraper {
  readonly source = "google_dorks";

  private readonly dorkTemplates = [
    '"{category}" "{city}" "contact us"',
    '"{category}" "{city}" "email" site:*.com',
    '"{category}" "{city}" inurl:contact',
    '"built with WordPress" "{category}" "{city}"',
    '"{category}" near "{city}" "phone"',
  ];

  async *scrape(
    job: ScrapeJob,
    signal: AbortSignal
  ): AsyncGenerator<RawLead[], void, unknown> {
    let batch: RawLead[] = [];
    const visitedUrls = new Set<string>();

    for (const template of this.dorkTemplates) {
      if (signal.aborted) break;

      const query = template
        .replace("{category}", job.category)
        .replace("{city}", job.city);

      for (let page = 0; page < MAX_PAGES && !signal.aborted; page++) {
        const start = page * RESULTS_PER_PAGE;
        const searchUrl = `https://www.google.com/search?q=${encodeURIComponent(query)}&start=${start}&num=${RESULTS_PER_PAGE}`;

        let html: string;
        try {
          const resp = await axios.get<string>(searchUrl, {
            timeout: 15_000,
            headers: {
              "User-Agent": randomUserAgent(),
              "Accept": "text/html,application/xhtml+xml",
              "Accept-Language": "en-US,en;q=0.9",
            },
            signal,
            responseType: "text",
          });
          html = resp.data;
        } catch (err) {
          if (signal.aborted) break;
          // Google often blocks with 429 — back off and move to next dork
          log.error("google dork fetch failed", { query, page, error: String(err) });
          break;
        }

        const $ = cheerio.load(html);
        const links: string[] = [];

        // Extract result URLs from SERP
        $("a[href]").each((_, el) => {
          const href = $(el).attr("href") ?? "";
          // Google wraps results in /url?q=<actual_url>
          const match = href.match(/\/url\?q=([^&]+)/);
          if (match) {
            try {
              const decoded = decodeURIComponent(match[1]);
              if (decoded.startsWith("http") && !decoded.includes("google.com")) {
                links.push(decoded);
              }
            } catch { /* malformed URL */ }
          }
        });

        if (links.length === 0) break;

        for (const siteUrl of links) {
          if (signal.aborted) break;

          const domain = this.extractDomain(siteUrl);
          if (!domain || visitedUrls.has(domain)) continue;
          visitedUrls.add(domain);

          const lead = await this.extractFromSite(siteUrl, job, signal);
          if (lead) {
            batch.push(lead);
            if (batch.length >= config.batchSize) {
              yield batch;
              batch = [];
            }
          }
        }

        await randomDelay();
      }

      // Extra delay between dork queries to avoid Google rate-limiting
      await randomDelay();
    }

    if (batch.length > 0) {
      yield batch;
    }
  }

  private async extractFromSite(
    url: string,
    job: ScrapeJob,
    signal: AbortSignal
  ): Promise<RawLead | null> {
    try {
      const { data } = await axios.get<string>(url, {
        timeout: 10_000,
        headers: { "User-Agent": randomUserAgent() },
        signal,
        maxRedirects: 3,
        responseType: "text",
      });

      const $ = cheerio.load(data);
      const text = $.text();
      const title = $("title").text().trim();

      const emails = extractEmails(text);
      const phones = extractPhones(text);

      // Need at least a business name to create a lead
      const businessName = title
        || $('meta[property="og:site_name"]').attr("content")
        || this.extractDomain(url);

      if (!businessName) return null;

      const techStack = await detectTechStack(url, signal);

      return {
        business_name: businessName,
        phone: phones[0] ?? null,
        email: emails[0] ?? null,
        website_url: url,
        address: null,
        city: job.city,
        country: "US",
        category: job.category,
        source: this.source,
        source_url: url,
        tech_stack: techStack,
        has_ssl: hasSSL(url),
        is_mobile_friendly: null,
      };
    } catch {
      return null;
    }
  }

  private extractDomain(url: string): string | null {
    try {
      return new URL(url).hostname.replace(/^www\./, "");
    } catch {
      return null;
    }
  }
}
