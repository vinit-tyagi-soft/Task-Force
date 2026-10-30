import type { NextConfig } from "next";

const apiOrigin = process.env.TASKFORCE_API_ORIGIN ?? "http://127.0.0.1:8080";

const nextConfig: NextConfig = {
  // Same-origin proxy keeps the HttpOnly, SameSite=Strict session cookie first-party.
  async rewrites() {
    return [{ source: "/api/:path*", destination: `${apiOrigin}/api/:path*` }];
  },
  async headers() {
    return [
      {
        source: "/:path*",
        headers: [
          { key: "X-Content-Type-Options", value: "nosniff" },
          { key: "Referrer-Policy", value: "same-origin" },
          { key: "X-Frame-Options", value: "DENY" },
        ],
      },
    ];
  },
};

export default nextConfig;
