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
        'features/auto-delete',
        'features/archive',
        'features/watch-folders',
        'features/bulk-actions',
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
