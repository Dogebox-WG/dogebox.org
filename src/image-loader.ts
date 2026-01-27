export default function loader({ src, width, quality }: { src: string; width?: number; quality?: number }) {
  // For static export, just return the src as-is
  return src;
}
