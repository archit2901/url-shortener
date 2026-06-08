import Link from "next/link";

import { Button } from "@/components/ui/button";

export default function NotFound() {
  return (
    <div className="min-h-screen flex items-center justify-center p-6">
      <div className="text-center space-y-4">
        <h1 className="text-4xl font-semibold tracking-tight">404</h1>
        <p className="text-muted-foreground">
          The page you&apos;re looking for doesn&apos;t exist.
        </p>
        <Link
            href="/"
            className="inline-flex items-center justify-center rounded-md text-sm font-medium h-10 px-4 bg-foreground text-background hover:bg-foreground/90 transition-colors"
            >
            Go home
        </Link>
      </div>
    </div>
  );
}