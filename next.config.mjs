import { createMDX } from 'fumadocs-mdx/next';

const withMDX = createMDX();

const isProduction = process.env.NODE_ENV === 'production';

/** @type {import('next').NextConfig} */
const config = {
  reactStrictMode: true,
  output: isProduction ? 'export' : undefined,
  images: isProduction
    ? {
        // required for static export
        loader: 'custom',
        loaderFile: './src/image-loader.ts',
      }
    : {
        // In dev mode, use default Next.js image optimization
        unoptimized: false,
      },
};

export default withMDX(config);
