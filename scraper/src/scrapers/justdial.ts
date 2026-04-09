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

/**
 * JustDial scraper — scrapes justdial.com for Indian business listings.
 * URL pattern: https://www.justdial.com/{city}/{category}
 */
const MAX_PAGES = 10;

export class JustDialScraper extends BaseScraper {
  readonly source = "justdial";

  async *scrape(
    job: ScrapeJob,
    signal: AbortSignal
  ): AsyncGenerator<RawLead[], void, unknown> {
    let batch: RawLead[] = [];

    // JustDial URL-encodes city and category into the path
    const citySlug = this.slugify(job.city);
    const categorySlug = this.slugify(job.category);

    for (let page = 1; page <= MAX_PAGES && !signal.aborted; page++) {
      const url =
        page === 1
          ? `https://www.justdial.com/${citySlug}/${categorySlug}`
          : `https://www.justdial.com/${citySlug}/${categorySlug}/page-${page}`;

      let html: string;
      try {
        const resp = await axios.get<string>(url, {
          timeout: 15_000,
          headers: {
            "User-Agent": randomUserAgent(),
            Accept: "text/html,application/xhtml+xml",
            "Accept-Language": "en-US,en;q=0.9",
            Referer: "https://www.justdial.com/",
          },
          signal,
          responseType: "text",
        });
        html = resp.data;
      } catch (err) {
        if (signal.aborted) break;
        log.error("justdial page fetch failed", {
          page,
          error: String(err),
        });
        break;
      }

      const $ = cheerio.load(html);

      // JustDial uses various listing card selectors
      const listings = $(".store-details, .resultbox_info, .cntanr, li[class*='resultbox']");

      if (listings.length === 0) {
        // Try alternative: JustDial sometimes uses JSP-based rendering with encoded numbers
        const altListings = $("[class*='store-details'], [class*='jcn']");
        if (altListings.length === 0) break;
      }

      // Parse main page text for listings
      const pageText = $.html() || "";
      const jsonLdScripts = $('script[type="application/ld+json"]');

      // Try JSON-LD structured data first (more reliable)
      for (let i = 0; i < jsonLdScripts.length && !signal.aborted; i++) {
        try {
          const jsonText = jsonLdScripts.eq(i).html();
          if (!jsonText) continue;

          const jsonData = JSON.parse(jsonText);
          const items = Array.isArray(jsonData) ? jsonData : [jsonData];

          for (const item of items) {
            if (
              item["@type"] !== "LocalBusiness" &&
              item["@type"] !== "Organization"
            )
              continue;

            const name = item.name;
            if (!name || typeof name !== "string") continue;

            const lead: RawLead = {
              business_name: name,
              phone: item.telephone || null,
              email: item.email || null,
              website_url: item.url || null,
              address: this.formatAddress(item.address),
              city: job.city,
              country: "IN",
              category: job.category,
              source: this.source,
              source_url: url,
              tech_stack: null,
              has_ssl: item.url ? hasSSL(item.url) : null,
              is_mobile_friendly: null,
            };

            batch.push(lead);

            if (batch.length >= config.batchSize) {
              yield batch;
              batch = [];
            }
          }
        } catch {
          // JSON parse failed, skip this script tag
        }
      }

      // Fallback: parse HTML listings
      listings.each((_, elem) => {
        if (signal.aborted) return;

        const el = $(elem);

        const name =
          el.find(".store-name, .lng_cont_name, .jcn").first().text().trim() ||
          el.find("a[title]").first().attr("title")?.trim() ||
          el.find("h2, h3").first().text().trim();

        if (!name || name.length < 2) return;

        // JustDial obfuscates phone numbers using CSS sprite replacement.
        // Try to get from data attributes or text.
        let phone: string | null = null;
        const phoneEl = el.find(".contact-info, .mobilesv, [class*='mobno']");
        if (phoneEl.length) {
          const phoneText = phoneEl.text().trim();
          const extracted = extractPhones(phoneText);
          phone = extracted[0] ?? null;
        }

        const address =
          el.find(".cont_sw_addr, .mljcont, .comp-address, [class*='addrs']")
            .first()
            .text()
            .trim() || null;

        let websiteUrl: string | null = null;
        const websiteLink = el.find("a[href*='http']:not([href*='justdial'])");
        if (websiteLink.length) {
          websiteUrl = websiteLink.attr("href") ?? null;
        }

        const lead: RawLead = {
          business_name: name,
          phone,
          email: null,
          website_url: websiteUrl,
          address,
          city: job.city,
          country: "IN",
          category: job.category,
          source: this.source,
          source_url: url,
          tech_stack: null,
          has_ssl: websiteUrl ? hasSSL(websiteUrl) : null,
          is_mobile_friendly: null,
        };

        batch.push(lead);
      });

      if (batch.length >= config.batchSize) {
        yield batch;
        batch = [];
      }

      await randomDelay();
    }

    // Enrich leads that have websites (batched at the end for efficiency)
    const enriched: RawLead[] = [];
    for (const lead of batch) {
      if (lead.website_url && !signal.aborted) {
        enriched.push(await this.enrichFromWebsite(lead, signal));
      } else {
        enriched.push(lead);
      }
    }

    if (enriched.length > 0) {
      yield enriched;
    }
  }

  private async enrichFromWebsite(
    lead: RawLead,
    signal: AbortSignal
  ): Promise<RawLead> {
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
      if (emails.length > 0 && !lead.email) lead.email = emails[0];

      if (!lead.phone) {
        const phones = extractPhones(data);
        if (phones.length > 0) lead.phone = phones[0];
      }

      lead.tech_stack = await detectTechStack(lead.website_url, signal);
      lead.has_ssl = hasSSL(lead.website_url);
    } catch {
      // best-effort
    }

    return lead;
  }

  private slugify(str: string): string {
    return str
      .trim()
      .replace(/\s+/g, "-")
      .replace(/[^\w-]/g, "");
  }

  private formatAddress(addr: unknown): string | null {
    if (!addr || typeof addr !== "object") return null;
    const a = addr as Record<string, string>;
    const parts = [a.streetAddress, a.addressLocality, a.addressRegion, a.postalCode];
    return parts.filter(Boolean).join(", ") || null;
  }
}
