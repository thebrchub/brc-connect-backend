import axios from "axios";
import { BaseScraper } from "./base.js";
import { ScrapeJob } from "../types/job.js";
import { RawLead } from "../types/lead.js";
import { config } from "../config.js";
import { randomUserAgent } from "../anti-ban/user-agents.js";
import { randomDelay } from "../anti-ban/delays.js";
import { extractEmails, extractPhones } from "../extractors/contact.js";
import { detectTechStack, hasSSL } from "../extractors/tech-stack.js";
import { log } from "../utils/logger.js";

/**
 * New Domains scraper — downloads daily CSV of newly registered domains
 * from whoisds.com, filters for business-looking domains, visits them
 * to extract contact info.
 */

const WHOISDS_BASE = "https://whoisds.com/newly-registered-domains";
const MAX_DOMAINS_TO_CHECK = 200;

export class NewDomainsScraper extends BaseScraper {
  readonly source = "new_domains";

  async *scrape(
    job: ScrapeJob,
    signal: AbortSignal
  ): AsyncGenerator<RawLead[], void, unknown> {
    let batch: RawLead[] = [];

    // Get today's date for the CSV download
    const today = new Date();
    const dateStr = today.toISOString().split("T")[0]; // YYYY-MM-DD

    let csvText: string;
    try {
      // whoisds provides daily CSV downloads of newly registered domains
      const resp = await axios.get<string>(
        `${WHOISDS_BASE}/${dateStr}/1`,
        {
          timeout: 30_000,
          headers: { "User-Agent": randomUserAgent() },
          signal,
          responseType: "text",
        }
      );
      csvText = resp.data;
    } catch (err) {
      if (signal.aborted) return;
      log.error("new domains CSV download failed", { date: dateStr, error: String(err) });
      return;
    }

    // Parse CSV — each line is a domain
    const lines = csvText.split("\n").map((l) => l.trim()).filter((l) => l.length > 0);

    // Filter domains related to the job category
    const categoryTerms = job.category.toLowerCase().split(/\s+/);
    const relevantDomains = lines
      .filter((domain) => {
        const lower = domain.toLowerCase().replace(/[^a-z0-9]/g, "");
        return categoryTerms.some((term) => lower.includes(term));
      })
      .slice(0, MAX_DOMAINS_TO_CHECK);

    for (const domain of relevantDomains) {
      if (signal.aborted) break;

      const cleanDomain = domain.replace(/["',]/g, "").trim();
      if (!cleanDomain || cleanDomain.length < 4) continue;

      const url = `https://${cleanDomain}`;
      const lead = await this.checkDomain(url, cleanDomain, job, signal);

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

  private async checkDomain(
    url: string,
    domain: string,
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
        validateStatus: (s) => s < 400,
      });

      const text = typeof data === "string" ? data : "";
      const emails = extractEmails(text);
      const phones = extractPhones(text);

      // Extract page title as business name
      const titleMatch = text.match(/<title[^>]*>([^<]+)<\/title>/i);
      const businessName = titleMatch?.[1]?.trim() || domain;

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
      return null; // domain not reachable
    }
  }
}
