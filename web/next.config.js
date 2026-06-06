/** @type {import('next').NextConfig} */
const nextConfig = {
  output: 'standalone',
  async rewrites() {
    return [
      {
        source: '/api/:path*',
        destination: process.env.SUWEN_API || 'http://localhost:9528/api/:path*',
      },
    ];
  },
};

module.exports = nextConfig;
