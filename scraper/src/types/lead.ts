/** RawLead — matches Go's models.RawLead exactly. Source-independent contract. */
export interface RawLead {
  business_name: string;
  phone?: string | null;
  email?: string | null;
  website_url?: string | null;
  address?: string | null;
  city: string;
  country: string;
  category: string;
  source: string;
  source_url?: string | null;
  tech_stack?: Record<string, string> | null;
  has_ssl?: boolean | null;
  is_mobile_friendly?: boolean | null;
  meta?: Record<string, string> | null;
}
