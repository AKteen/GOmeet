import type { NextConfig } from "next";
import path from "path";

const nextConfig: NextConfig = {
  // Docker Compose needs a standalone server. Vercel uses its own build output.
  ...(process.env.DOCKER_BUILD === "1" ? { output: "standalone" as const } : {}),
  outputFileTracingRoot: path.join(__dirname),
};

export default nextConfig;
