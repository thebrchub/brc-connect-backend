import axios from "axios";
import * as cheerio from "cheerio";
import { BaseScraper } from "./base.js";
import { ScrapeJob } from "../types/job.js";
import { RawLead } from "../types/lead.js";
import { config } from "../config.js";
import { randomUserAgent } from "../anti-ban/user-agents.js";
import { randomDelay, sleep } from "../anti-ban/delays.js";
import { extractEmails, extractPhones } from "../extractors/contact.js";
import { detectTechStack, hasSSL } from "../extractors/tech-stack.js";
import { log } from "../utils/logger.js";

/**
 * LinkedIn scraper — uses Google Dorks to find LinkedIn company pages,
 * then extracts business info from the public profile snippets.
 *
 * LinkedIn blocks direct scraping aggressively, so we search via
 * Google: site:linkedin.com/company "{category}" "{city}"
 */
const MAX_PAGES = 5;
const RESULTS_PER_PAGE = 10;

export class LinkedInScraper extends BaseScraper {
  readonly source = "linkedin";

  async *scrape(
    job: ScrapeJob,
    signal: AbortSignal
  ): AsyncGenerator<RawLead[], void, unknown> {
    let batch: RawLead[] = [];

    for (let page = 0; page < MAX_PAGES && !signal.aborted; page++) {
      const start = page * RESULTS_PER_PAGE;
      const query = `site:linkedin.com/company "${job.category}" "${job.city}"`;
      const url = `https://www.google.com/search?q=${encodeURIComponent(query)}&start=${start}&num=${RESULTS_PER_PAGE}`;

      let html: string;
      try {
        const resp = await axios.get<string>(url, {
          timeout: 15_000,
          headers: {
            "User-Agent": randomUserAgent(),
            Accept: "text/html,application/xhtml+xml",
            "Accept-Language": "en-US,en;q=0.9",
          },
          signal,
          responseType: "text",
        });
        html = resp.data;
      } catch (err) {
        if (signal.aborted) break;
        log.error("linkedin google search failed", { page, error: String(err) });
        break;
      }

      const $ = cheerio.load(html);
      const results = $("div.g");

      if (results.length === 0) break;

      for (let i = 0; i < results.length && !signal.aborted; i++) {
        const el = results.eq(i);
        const link = el.find("a").first().attr("href") ?? "";

        // Only process linkedin.com/company links
        if (!link.includes("linkedin.com/company")) continue;

        const titleEl = el.find("h3").first();
        const snippetEl = el.find(".VwiC3b, [data-sncf]").first();

        let name = titleEl.text().trim();
        // LinkedIn titles are like "Company Name - LinkedIn"
        name = name.replace(/\s*[-|·]\s*(LinkedIn|Overview).*$/i, "").trim();
        if (!name || name.length < 2) continue;

        const snippet = snippetEl.text().trim();

        // Try to extract location/address from snippet
        let address: string | null = null;
        const locMatch = snippet.match(
          /(?:located?\s+(?:in|at)|headquarters?\s+(?:in|at)?)\s+([^.]+)/i
        );
        if (locMatch) address = locMatch[1].trim();

        const lead: RawLead = {
          business_name: name,
          phone: null,
          email: null,
          website_url: link,
          address,
          city: job.city,
          country: detectCountryFromCity(job.city),
          category: job.category,
          source: this.source,
          source_url: link,
          tech_stack: null,
          has_ssl: true, // LinkedIn is always HTTPS
          is_mobile_friendly: null,
        };

        // Try to scrape the actual LinkedIn page for more info
        if (!signal.aborted) {
          const enriched = await this.enrichFromLinkedIn(lead, link, signal);
          batch.push(enriched);
        } else {
          batch.push(lead);
        }

        if (batch.length >= config.batchSize) {
          yield batch;
          batch = [];
        }
      }

      // Longer delays for Google to avoid rate limiting
      await sleep(3000 + Math.random() * 3000);
    }

    if (batch.length > 0) {
      yield batch;
    }
  }

  private async enrichFromLinkedIn(
    lead: RawLead,
    linkedinUrl: string,
    signal: AbortSignal
  ): Promise<RawLead> {
    try {
      const { data } = await axios.get<string>(linkedinUrl, {
        timeout: 10_000,
        headers: {
          "User-Agent": randomUserAgent(),
          Accept: "text/html,application/xhtml+xml",
        },
        signal,
        maxRedirects: 3,
        responseType: "text",
      });

      const $ = cheerio.load(data);

      // Extract website from LinkedIn company page
      const websiteEl = $('a[data-tracking-control-name="about_website"]').first();
      const externalUrl =
        websiteEl.attr("href") ??
        $(".link-without-visited-state").first().attr("href") ??
        null;

      if (externalUrl && !externalUrl.includes("linkedin.com")) {
        lead.website_url = externalUrl;

        // Enrich from actual website
        if (!signal.aborted) {
          await this.enrichFromWebsite(lead, signal);
        }
      }

      // Try extracting phone/email from page text
      const pageText = $.text();
      if (!lead.phone) {
        const phones = extractPhones(pageText);
        if (phones.length > 0) lead.phone = phones[0];
      }
      if (!lead.email) {
        const emails = extractEmails(pageText);
        if (emails.length > 0) lead.email = emails[0];
      }
    } catch {
      // Enrichment is best-effort; LinkedIn may block
    }

    return lead;
  }

  private async enrichFromWebsite(
    lead: RawLead,
    signal: AbortSignal
  ): Promise<void> {
    if (!lead.website_url || signal.aborted) return;

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
  }
}

function detectCountryFromCity(city: string): string {
  const lower = city.toLowerCase();
  const indianCities = [
    "mumbai", "delhi", "bangalore", "bengaluru", "chennai", "kolkata",
    "hyderabad", "pune", "ahmedabad", "jaipur", "lucknow", "kanpur",
    "nagpur", "indore", "thane", "bhopal", "visakhapatnam", "pimpri",
    "patna", "vadodara", "ghaziabad", "ludhiana", "agra", "nashik",
    "faridabad", "meerut", "rajkot", "varanasi", "srinagar", "aurangabad",
    "dhanbad", "amritsar", "navi mumbai", "allahabad", "howrah", "ranchi",
    "gwalior", "jabalpur", "coimbatore", "vijayawada", "jodhpur", "madurai",
    "raipur", "kota", "chandigarh", "guwahati", "solapur", "hubli",
    "mysore", "mysuru", "tiruchirappalli", "bareilly", "noida", "gurgaon",
    "gurugram",
  ];
  if (indianCities.some((c) => lower.includes(c))) return "IN";
  return "US";
}
