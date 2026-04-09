import axios from "axios";
import { BaseScraper } from "./base.js";
import { ScrapeJob } from "../types/job.js";
import { RawLead } from "../types/lead.js";
import { config } from "../config.js";
import { randomUserAgent } from "../anti-ban/user-agents.js";
import { randomDelay } from "../anti-ban/delays.js";
import { extractEmails, extractPhones } from "../extractors/contact.js";
import { log } from "../utils/logger.js";

/**
 * Reddit scraper — uses old.reddit.com JSON API (no auth needed).
 * Searches for posts about needing services in a category/city.
 */

const BASE_URL = "https://old.reddit.com";
const MAX_PAGES = 5;

interface RedditPost {
  data: {
    title: string;
    selftext: string;
    url: string;
    author: string;
    permalink: string;
    subreddit: string;
    created_utc: number;
  };
}

interface RedditListing {
  data: {
    children: RedditPost[];
    after: string | null;
  };
}

export class RedditScraper extends BaseScraper {
  readonly source = "reddit";

  private readonly searchQueries = [
    '"{category}" "{city}" need',
    '"{category}" "{city}" looking for',
    '"{category}" "{city}" recommend',
    '"{category}" "{city}" hiring',
    '"need a website" "{city}"',
    '"looking for developer" "{city}"',
  ];

  async *scrape(
    job: ScrapeJob,
    signal: AbortSignal
  ): AsyncGenerator<RawLead[], void, unknown> {
    let batch: RawLead[] = [];
    const seenUrls = new Set<string>();

    for (const queryTemplate of this.searchQueries) {
      if (signal.aborted) break;

      const query = queryTemplate
        .replace("{category}", job.category)
        .replace("{city}", job.city);

      let after: string | null = null;

      for (let page = 0; page < MAX_PAGES && !signal.aborted; page++) {
        const searchUrl = `${BASE_URL}/search.json?q=${encodeURIComponent(query)}&sort=new&limit=25${after ? `&after=${after}` : ""}`;

        let listing: RedditListing;
        try {
          const resp = await axios.get<RedditListing>(searchUrl, {
            timeout: 15_000,
            headers: {
              "User-Agent": randomUserAgent(),
              "Accept": "application/json",
            },
            signal,
          });
          listing = resp.data;
        } catch (err) {
          if (signal.aborted) break;
          log.error("reddit search failed", { query, page, error: String(err) });
          break;
        }

        const posts = listing.data.children;
        if (posts.length === 0) break;
        after = listing.data.after;

        for (const post of posts) {
          if (signal.aborted) break;

          const permalink = `https://www.reddit.com${post.data.permalink}`;
          if (seenUrls.has(permalink)) continue;
          seenUrls.add(permalink);

          const fullText = `${post.data.title} ${post.data.selftext}`;
          const emails = extractEmails(fullText);
          const phones = extractPhones(fullText);

          // Extract any external URL from the post
          const externalUrl = post.data.url.startsWith("https://www.reddit.com")
            ? null
            : post.data.url.startsWith("http")
              ? post.data.url
              : null;

          const lead: RawLead = {
            business_name: post.data.title.slice(0, 200),
            phone: phones[0] ?? null,
            email: emails[0] ?? null,
            website_url: externalUrl,
            address: null,
            city: job.city,
            country: "US",
            category: job.category,
            source: this.source,
            source_url: `https://reddit.com${permalink}`,
            tech_stack: null,
            has_ssl: null,
            is_mobile_friendly: null,
            meta: {
              reddit_url: permalink,
              subreddit: post.data.subreddit,
              author: post.data.author,
            },
          };

          batch.push(lead);

          if (batch.length >= config.batchSize) {
            yield batch;
            batch = [];
          }
        }

        if (!after) break; // no more pages
        await randomDelay();
      }
    }

    if (batch.length > 0) {
      yield batch;
    }
  }
}
