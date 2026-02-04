/** @type {import('@docusaurus/plugin-content-docs').SidebarsConfig} */
const sidebars = {
  docsSidebar: [
    'intro',
    {
      type: 'category',
      label: 'Getting Started',
      collapsed: false,
      items: [
        'getting-started/installation',
        'getting-started/quick-start',
        'getting-started/keyboard-shortcuts',
      ],
    },
    {
      type: 'category',
      label: 'Features',
      collapsed: false,
      items: [
        'features/clipboard-management',
        'features/image-editor',
        'features/image-comparison',
        'features/text-editor',
        'features/tags',
        'features/auto-delete',
        'features/archive',
        'features/watch-folders',
        'features/bulk-actions',
      ],
    },
    {
      type: 'category',
      label: 'Plugins',
      collapsed: false,
      items: [
        'plugins/overview',
        'plugins/installing-plugins',
        {
          type: 'category',
          label: 'Writing Plugins',
          collapsed: true,
          items: [
            'plugins/writing/getting-started',
            'plugins/writing/plugin-manifest',
            'plugins/writing/event-handling',
            'plugins/writing/settings-storage',
          ],
        },
        'plugins/api-reference',
        'plugins/example-plugins',
      ],
    },
    {
      type: 'category',
      label: 'Tutorials',
      collapsed: true,
      items: [
        'tutorials/screenshot-workflow',
        'tutorials/code-snippets',
        'tutorials/automated-imports',
      ],
    },
    {
      type: 'category',
      label: 'Developers',
      collapsed: true,
      items: [
        'developers/architecture',
        'developers/frontend',
        'developers/backend',
        'developers/database-schema',
        'developers/api-reference',
        'developers/contributing',
      ],
    },
    {
      type: 'category',
      label: 'Reference',
      collapsed: true,
      items: [
        'reference/data-storage',
        'reference/troubleshooting',
      ],
    },
  ],
};

export default sidebars;
