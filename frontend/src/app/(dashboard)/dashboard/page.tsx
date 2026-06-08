"use client";

import Link from "next/link";
import { useCallback, useEffect, useState } from "react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";

import { api, ApiError } from "@/lib/api";
import { URLListItem } from "@/lib/types";

export default function DashboardPage() {
  const [url, setUrl] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [urls, setUrls] = useState<URLListItem[]>([]);
  const [loadingUrls, setLoadingUrls] = useState(true);

  const refreshUrls = useCallback(async () => {
    try {
      const res = await api.listURLs();
      setUrls(res.urls ?? []);
    } catch (err) {
      const msg = err instanceof ApiError ? err.message : "failed to load URLs";
      toast.error(msg);
    } finally {
      setLoadingUrls(false);
    }
  }, []);

  useEffect(() => {
    refreshUrls();
  }, [refreshUrls]);

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!url.trim()) return;
    setSubmitting(true);
    try {
      const res = await api.shorten(url.trim());
      toast.success(`Short URL created: ${res.short_code}`);
      setUrl("");
      // Refresh the list to show the new URL
      refreshUrls();
    } catch (err) {
      const msg = err instanceof ApiError ? err.message : "failed to shorten URL";
      toast.error(msg);
    } finally {
      setSubmitting(false);
    }
  }

  async function copyToClipboard(text: string) {
    try {
      await navigator.clipboard.writeText(text);
      toast.success("Copied to clipboard");
    } catch {
      toast.error("Could not copy");
    }
  }

  return (
    <div className="space-y-8">
      <Card>
        <CardHeader>
          <CardTitle>Shorten a URL</CardTitle>
          <CardDescription>
            Paste a long URL. We&apos;ll give you a short code and track clicks.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <form onSubmit={onSubmit} className="flex flex-col sm:flex-row gap-2">
            <Input
              type="url"
              placeholder="https://example.com/some/long/path"
              value={url}
              onChange={(e) => setUrl(e.target.value)}
              required
              className="flex-1"
            />
            <Button type="submit" disabled={submitting || !url.trim()}>
              {submitting ? "Shortening..." : "Shorten"}
            </Button>
          </form>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Your URLs</CardTitle>
          <CardDescription>
            {urls.length > 0
              ? `${urls.length} link${urls.length === 1 ? "" : "s"}`
              : "No links yet — shorten one above to get started."}
          </CardDescription>
        </CardHeader>
        <CardContent>
          {loadingUrls ? (
            <div className="space-y-2">
              <Skeleton className="h-12 w-full" />
              <Skeleton className="h-12 w-full" />
              <Skeleton className="h-12 w-full" />
            </div>
          ) : urls.length === 0 ? null : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Short URL</TableHead>
                  <TableHead className="hidden md:table-cell">Destination</TableHead>
                  <TableHead className="text-right">Clicks</TableHead>
                  <TableHead className="text-right">Stats</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {urls.map((u) => (
                  <TableRow key={u.short_code}>
                    <TableCell className="font-mono">
                      <button
                        onClick={() => copyToClipboard(u.short_url)}
                        className="hover:underline text-left"
                        title="Click to copy"
                      >
                        /{u.short_code}
                      </button>
                    </TableCell>
                    <TableCell className="hidden md:table-cell text-muted-foreground truncate max-w-md">
                      {u.long_url}
                    </TableCell>
                    <TableCell className="text-right tabular-nums">
                      {u.click_count.toLocaleString()}
                    </TableCell>
                    <TableCell className="text-right">
                      <Link
                        href={`/dashboard/${u.short_code}`}
                        className="text-sm underline hover:text-foreground text-muted-foreground"
                      >
                        View
                      </Link>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </div>
  );
}