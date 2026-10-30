import type { Metadata, Viewport } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "TaskForce — Your work, beautifully in sync",
  description: "TaskForce — the enterprise work command center. Plan smarter. Execute faster. Lead better.",
};

export const viewport: Viewport = { themeColor: "#7160e8" };

export default function RootLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  return (
    <html lang="en" data-theme="light">
      <body>{children}</body>
    </html>
  );
}
