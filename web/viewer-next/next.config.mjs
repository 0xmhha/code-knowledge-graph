/** @type {import('next').NextConfig} */
const nextConfig = {
  reactStrictMode: false,
  output: 'export',
  images: { unoptimized: true },
  trailingSlash: true,
  // Dev-only: proxy /api/* to local Go server. Ignored by static export build.
  async rewrites() {
    return [
      { source: '/api/:path*', destination: 'http://localhost:8080/api/:path*' },
    ];
  },
};

export default nextConfig;
