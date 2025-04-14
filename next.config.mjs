import { createMDX } from 'fumadocs-mdx/next';

const withMDX = createMDX();

/** @type {import('next').NextConfig} */
const config = {
  reactStrictMode: true,
  output: 'export',
  images: {
    // required for static export
    loader: 'custom',
    loaderFile: './src/image-loader.ts',
  }
};

export default withMDX(config);
