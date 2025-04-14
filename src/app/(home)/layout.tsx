import type { ReactNode } from 'react';
import { HomeLayout } from 'fumadocs-ui/layouts/home';
import { baseOptions } from '@/app/layout.config';
import { BaseLayoutProps } from 'fumadocs-ui/layouts/shared';

export default function Layout({ children }: { children: ReactNode }) {
  const options: BaseLayoutProps = {
    ...baseOptions,
    links: [
      {
        text: 'Usage',
        type: 'main',
        url: '/docs/usage',
       },
       {
        text: 'Dogebox Development',
        type: 'main',
        url: '/docs/dogebox',
       },
       {
        text: 'Pup Development',
        type: 'main',
        url: '/docs/pup',
       },
       {
        text: 'DKM',
        type: 'main',
        url: '/docs/dkm',
       },
       {
        text: 'Dogenet',
        type: 'main',
        url: '/docs/dogenet',
       },
    ],
  };

  return <HomeLayout {...options}>{children}</HomeLayout>;
}
