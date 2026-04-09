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

const BASE_URL = "https://www.yelp.com/search";
const MAX_PAGES = 10;
const RESULTS_PER_PAGE = 10;

export class YelpScraper extends BaseScraper {
  readonly source = "yelp";

  async *scrape(
    job: ScrapeJob,
    signal: AbortSignal
  ): AsyncGenerator<RawLead[], void, unknown> {
    let batch: RawLead[] = [];

    for (let page = 0; page < MAX_PAGES && !signal.aborted; page++) {
      const start = page * RESULTS_PER_PAGE;
      const url = `${BASE_URL}?find_desc=${encodeURIComponent(job.category)}&find_loc=${encodeURIComponent(job.city)}&start=${start}`;

      let html: string;
      try {
        const resp = await axios.get<string>(url, {
          timeout: 15_000,
          headers: { "User-Agent": randomUserAgent() },
          signal,
          responseType: "text",
        });
        html = resp.data;
      } catch (err) {
        if (signal.aborted) break;
        log.error("yelp page fetch failed", { page, error: String(err) });
        break;
      }

      const $ = cheerio.load(html);
      const listings = $('[data-testid="serp-ia-card"]');

      if (listings.length === 0) break; // no more results

      for (let i = 0; i < listings.length && !signal.aborted; i++) {
        const el = listings.eq(i);

        const nameLink = el.find("a[class*='businessName']");
        const name = nameLink.text().trim()
          || el.find("h3 a, h4 a").first().text().trim();
        if (!name) continue;

        const listingHref = nameLink.attr("href") || el.find("h3 a, h4 a").first().attr("href") || null;
        const listingUrl = listingHref ? `https://www.yelp.com${listingHref}` : null;

        const phone = el.find('[class*="phone"]').text().trim() || null;
        const address = el.find("address, [class*='secondaryAttributes'] span").text().trim() || null;
        const websiteLink = el.find('a[href*="/biz_redir"]').attr("href") ?? null;
        let websiteUrl: string | null = null;
        if (websiteLink) {
          try {
            const parsed = new URL(websiteLink, "https://www.yelp.com");
            websiteUrl = parsed.searchParams.get("url") ?? websiteLink;
          } catch {
            websiteUrl = websiteLink;
          }
        }

        const lead: RawLead = {
          business_name: name,
          phone: phone ? extractPhones(phone)[0] ?? phone : null,
          email: null,
          website_url: websiteUrl,
          address,
          city: job.city,
          country: "US",
          category: job.category,
          source: this.source,
          source_url: listingUrl,
          tech_stack: null,
          has_ssl: websiteUrl ? hasSSL(websiteUrl) : null,
          is_mobile_friendly: null,
        };

        // Enrich from website
        if (lead.website_url && !signal.aborted) {
          const enriched = await this.enrichFromWebsite(lead, signal);
          batch.push(enriched);
        } else {
          batch.push(lead);
        }

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

  private async enrichFromWebsite(lead: RawLead, signal: AbortSignal): Promise<RawLead> {
    if (!lead.website_url || signal.aborted) return lead;

    try {
      const { data } = await axios.get<string>(lead.website_url, {
        timeout: 10_000,
        headers: { "User-Agent": randomUserAgent() },
        signal,
        maxRedirects: 3,
        responseType: "text",
      });

      const emails = extractEmails(data);
      if (emails.length > 0) lead.email = emails[0];

      if (!lead.phone) {
        const phones = extractPhones(data);
        if (phones.length > 0) lead.phone = phones[0];
      }

      lead.tech_stack = await detectTechStack(lead.website_url, signal);
      lead.has_ssl = hasSSL(lead.website_url);
    } catch {
      // enrichment is best-effort
    }

    return lead;
  }
}
