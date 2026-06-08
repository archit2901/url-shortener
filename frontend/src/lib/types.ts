export interface User {
  id: string;
  email: string;
}

export interface AuthResponse {
  token?: string;
  user: User;
}

export interface ShortenResponse {
  short_code: string;
  short_url: string;
  long_url: string;
}

export interface URLListItem {
  short_code: string;
  short_url: string;
  long_url: string;
  click_count: number;
  created_at: string;
}

export interface URLListResponse {
  urls: URLListItem[];
  limit: number;
  offset: number;
}

export interface DailyClicks {
  day: string;
  clicks: number;
}

export interface ClickStats {
  total_clicks: number;
  unique_visitors: number;
  clicks_by_day: DailyClicks[];
}

export interface ApiError {
  error: string;
}