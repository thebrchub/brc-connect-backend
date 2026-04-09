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

const BASE_URL = "https://www.yellowpages.com/search";
const MAX_PAGES = 10;

export class YellowPagesScraper extends BaseScraper {
  readonly source = "yellow_pages";

  async *scrape(
    job: ScrapeJob,
    signal: AbortSignal
  ): AsyncGenerator<RawLead[], void, unknown> {
    let batch: RawLead[] = [];

    for (let page = 1; page <= MAX_PAGES && !signal.aborted; page++) {
      const url = `${BASE_URL}?search_terms=${encodeURIComponent(job.category)}&geo_location_terms=${encodeURIComponent(job.city)}&page=${page}`;

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
        log.error("yellow pages fetch failed", { page, error: String(err) });
        break;
      }

      const $ = cheerio.load(html);
      const listings = $(".result");

      if (listings.length === 0) break;

      for (let i = 0; i < listings.length && !signal.aborted; i++) {
        const el = listings.eq(i);

        const nameLink = el.find(".business-name a, .n a");
        const name = nameLink.text().trim();
        if (!name) continue;

        const listingHref = nameLink.attr("href") || null;
        const listingUrl = listingHref ? `https://www.yellowpages.com${listingHref}` : null;

        const phoneRaw = el.find(".phones, .phone").text().trim();
        const addressParts: string[] = [];
        const street = el.find(".street-address, .adr .street-address").text().trim();
        const locality = el.find(".locality, .adr .locality").text().trim();
        if (street) addressParts.push(street);
        if (locality) addressParts.push(locality);

        const websiteUrl = el.find('a.track-visit-website, a[href*="website"]').attr("href") ?? null;

        const lead: RawLead = {
          business_name: name,
          phone: phoneRaw ? extractPhones(phoneRaw)[0] ?? phoneRaw : null,
          email: null,
          website_url: websiteUrl,
          address: addressParts.length > 0 ? addressParts.join(", ") : null,
          city: job.city,
          country: "US",
          category: job.category,
          source: this.source,
          source_url: listingUrl,
          tech_stack: null,
          has_ssl: websiteUrl ? hasSSL(websiteUrl) : null,
          is_mobile_friendly: null,
        };

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
