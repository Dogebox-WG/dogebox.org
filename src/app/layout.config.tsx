import type { BaseLayoutProps } from 'fumadocs-ui/layouts/shared';
import Image from 'next/image';
/**
 * Shared layout configurations
 *
 * you can customise layouts individually from:
 * Home Layout: app/(home)/layout.tsx
 * Docs Layout: app/docs/layout.tsx
 */
export const baseOptions: BaseLayoutProps = {
  nav: {
    title: (
      <>
        <Image
          src="/static/image/dogebox-logo.png"
          alt="Dogebox Logo"
          width={32}
          height={32}
          className="mr-2"
        />{' '}
        Dogebox
      </>
    ),
  },
  links: [],
};
