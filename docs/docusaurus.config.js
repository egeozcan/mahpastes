// @ts-check
import { themes as prismThemes } from 'prism-react-renderer';

/** @type {import('@docusaurus/types').Config} */
const config = {
  title: 'mahpastes',
  tagline: 'Your local clipboard manager',
  favicon: 'img/favicon.ico',

  url: 'https://egeozcan.github.io',
  baseUrl: '/mahpastes/',

  organizationName: 'egeozcan',
  projectName: 'mahpastes',

  onBrokenLinks: 'throw',
  onBrokenMarkdownLinks: 'warn',

  i18n: {
    defaultLocale: 'en',
    locales: ['en'],
  },

  presets: [
    [
      'classic',
      /** @type {import('@docusaurus/preset-classic').Options} */
      ({
        docs: {
          sidebarPath: './sidebars.js',
          routeBasePath: '/',
          editUrl: 'https://github.com/egeozcan/mahpastes/tree/master/docs/',
        },
        blog: false,
        theme: {
          customCss: './src/css/custom.css',
        },
      }),
    ],
  ],

  themeConfig:
    /** @type {import('@docusaurus/preset-classic').ThemeConfig} */
    ({
      image: 'img/social-card.png',
      navbar: {
        title: 'mahpastes',
        logo: {
          alt: 'mahpastes Logo',
          src: 'img/logo.png',
        },
        items: [
          {
            type: 'docSidebar',
            sidebarId: 'docsSidebar',
            position: 'left',
            label: 'Docs',
          },
          {
            href: 'https://github.com/egeozcan/mahpastes',
            label: 'GitHub',
            position: 'right',
          },
        ],
      },
      footer: {
        style: 'dark',
        links: [
          {
            title: 'Documentation',
            items: [
              {
                label: 'Getting Started',
                to: '/getting-started/installation',
              },
              {
                label: 'Features',
                to: '/features/clipboard-management',
              },
              {
                label: 'Tutorials',
                to: '/tutorials/screenshot-workflow',
              },
            ],
          },
          {
            title: 'Developers',
            items: [
              {
                label: 'Architecture',
                to: '/developers/architecture',
              },
              {
                label: 'API Reference',
                to: '/developers/api-reference',
              },
              {
                label: 'Contributing',
                to: '/developers/contributing',
              },
            ],
          },
          {
            title: 'More',
            items: [
              {
                label: 'GitHub',
                href: 'https://github.com/egeozcan/mahpastes',
              },
              {
                label: 'Releases',
                href: 'https://github.com/egeozcan/mahpastes/releases',
              },
            ],
          },
        ],
        copyright: `Copyright ${new Date().getFullYear()} mahpastes. Built with Docusaurus.`,
      },
      prism: {
        theme: prismThemes.github,
        darkTheme: prismThemes.dracula,
        additionalLanguages: ['bash', 'go', 'json'],
      },
      colorMode: {
        defaultMode: 'light',
        disableSwitch: false,
        respectPrefersColorScheme: true,
      },
    }),
};

export default config;
