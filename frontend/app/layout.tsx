import type { Metadata } from "next";

import { APP_VERSION } from "@/lib/version";

import "./globals.css";

export const metadata: Metadata = {
  title: "GOmeet",
  description: "One-to-one and one-to-many video calling",
  icons: {
    icon: "/gomeet.png",
    apple: "/gomeet.png",
  },
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="en">
      <head>
        <link rel="preconnect" href="https://fonts.googleapis.com" />
        <link rel="preconnect" href="https://fonts.gstatic.com" crossOrigin="" />
        <link
          href="https://fonts.googleapis.com/css2?family=Google+Sans:wght@400;500;700&family=Material+Symbols+Outlined:opsz,wght,FILL,GRAD@24,500,0,0&display=swap"
          rel="stylesheet"
        />
        <link rel="icon" href="/gomeet.png" type="image/png" />
      </head>
      <body className="antialiased">
        <p className="pointer-events-none fixed right-4 top-4 z-50 text-xs tracking-wide text-zinc-500">
          v{APP_VERSION}
        </p>
        {children}
      </body>
    </html>
  );
}
