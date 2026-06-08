"use client";

import { useEffect } from "react";

export function usePageTitle(title: string) {
  useEffect(() => {
    const prev = document.title;
    document.title = title ? `${title} — URL Shortener` : "URL Shortener";
    return () => {
      document.title = prev;
    };
  }, [title]);
}