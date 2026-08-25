import { defineConfig } from 'vitepress'

const base = process.env.KKRICH_DOCS_BASE || '/docs/'

export default defineConfig({
  lang: 'zh-CN',
  title: 'KKRICH 文档',
  titleTemplate: ':title | KKRICH 文档',
  description: 'KKRICH 控制台、AI 应用与 API 接入文档',
  base,
  outDir: './.vitepress/dist-docs-path',
  cleanUrls: true,
  lastUpdated: true,
  sitemap: {
    hostname: 'https://api.kkrich.ltd/docs/'
  },
  head: [
    ['link', { rel: 'icon', type: 'image/svg+xml', href: `${base}favicon.svg` }],
    ['meta', { name: 'theme-color', content: '#0f766e' }],
    ['meta', { property: 'og:type', content: 'website' }],
    ['meta', { property: 'og:site_name', content: 'KKRICH 文档' }],
    ['meta', { property: 'og:title', content: 'KKRICH 文档' }],
    ['meta', { property: 'og:description', content: '面向控制台、AI 应用与 API 接入的 KKRICH 技术文档。' }],
    ['meta', { property: 'og:image', content: 'https://api.kkrich.ltd/docs/og.svg' }]
  ],
  themeConfig: {
    logo: { src: '/logo.svg', alt: 'KKRICH' },
    siteTitle: 'KKRICH 文档',
    externalLinkIcon: true,
    nav: [
      { text: '首页', link: '/' },
      { text: '使用指南', link: '/guide/' },
      { text: 'AI 应用', link: '/apps/' },
      { text: 'API 接入', link: '/api/' },
      { text: '附加资源', link: '/resources/' },
      { text: '前往官网', link: 'https://api.kkrich.ltd' },
      { text: '帮助支持', link: '/support/' }
    ],
    sidebar: {
      '/guide/': [
        {
          text: '开始使用',
          collapsed: false,
          items: [
            { text: '使用指南', link: '/guide/' },
            { text: '新手上手', link: '/guide/new-user' },
            { text: 'API 快速开始', link: '/guide/quickstart' },
            { text: '控制台', link: '/guide/console' },
            { text: '仪表盘', link: '/guide/dashboard' }
          ]
        },
        {
          text: '账户与资源',
          collapsed: false,
          items: [
            { text: 'API Token', link: '/guide/api-token' },
            { text: '钱包充值', link: '/guide/wallet' },
            { text: '定价', link: '/guide/pricing' },
            { text: '个人资料', link: '/guide/profile' }
          ]
        },
        {
          text: '控制台能力',
          collapsed: false,
          items: [
            { text: 'Playground', link: '/guide/playground' },
            { text: '聊天', link: '/guide/chat' },
            { text: '使用日志', link: '/guide/usage-log' },
            { text: '任务日志', link: '/guide/task-log' },
            { text: '绘图日志', link: '/guide/drawing-log' }
          ]
        }
      ],
      '/apps/': [
        {
          text: '生成能力',
          collapsed: false,
          items: [
            { text: 'Seedance 视频（2.0 / 2.5）', link: '/apps/seedance' },
            { text: 'image2 生图入门', link: '/apps/image2' }
          ]
        },
        {
          text: '总览与通用规则',
          collapsed: false,
          items: [
            { text: 'AI 应用概览', link: '/apps/' },
            { text: 'OpenAI 兼容工具', link: '/apps/openai-compatible-tools' },
            { text: 'CC Switch', link: '/apps/cc-switch' },
            { text: '手动切换配置', link: '/apps/cc-switch-manual' }
          ]
        },
        {
          text: '代码与命令行',
          collapsed: false,
          items: [
            { text: 'Codex', link: '/apps/codex' },
            { text: 'Codex CLI', link: '/apps/codex-cli' },
            { text: 'Claude Code', link: '/apps/claude-code' },
            { text: 'CCR', link: '/apps/ccr' },
            { text: 'Factory Droid CLI', link: '/apps/factory-droid-cli' },
            { text: 'Cursor', link: '/apps/cursor' },
            { text: 'JetBrains', link: '/apps/jetbrains' }
          ]
        },
        {
          text: '客户端与机器人',
          collapsed: false,
          items: [
            { text: 'Cherry Studio', link: '/apps/cherry-studio' },
            { text: 'AionUi', link: '/apps/aionui' },
            { text: 'Fluent Read', link: '/apps/fluent-read' },
            { text: 'Luna Translator', link: '/apps/luna-translator' },
            { text: 'LangBot', link: '/apps/langbot' },
            { text: 'AstrBot', link: '/apps/astrbot' },
            { text: 'Memoh', link: '/apps/memoh' },
            { text: 'OpenClaw', link: '/apps/openclaw' }
          ]
        },
        {
          text: '接口调试',
          collapsed: false,
          items: [
            { text: 'Apifox', link: '/apps/apifox' },
            { text: 'Postman', link: '/apps/postman' }
          ]
        }
      ],
      '/api/': [
        {
          text: 'API 接入',
          collapsed: false,
          items: [
            { text: 'API 接入概览', link: '/api/' },
            { text: 'Base URL', link: '/api/base-url' },
            { text: '认证', link: '/api/authentication' },
            { text: '接口端点', link: '/api/endpoints' },
            { text: '模型列表', link: '/api/models' },
            { text: '额度与限速', link: '/api/rate-limits' },
            { text: '错误处理', link: '/api/errors' }
          ]
        },
        {
          text: '生成接口',
          collapsed: false,
          items: [
            { text: 'Seedance 视频生成', link: '/api/video-generation' }
          ]
        },
        {
          text: '调用示例',
          collapsed: false,
          items: [
            { text: 'Chat Completions', link: '/api/chat-completions' },
            { text: 'Responses', link: '/api/responses' },
            { text: 'cURL', link: '/api/curl' },
            { text: 'Node.js', link: '/api/nodejs' },
            { text: 'Python', link: '/api/python' }
          ]
        }
      ],
      '/resources/': [
        {
          text: '附加资源',
          collapsed: false,
          items: [
            { text: '附加资源概览', link: '/resources/' },
            { text: '文档导航', link: '/resources/docs-navigation' },
            { text: '站点链接', link: '/resources/site-links' },
            { text: 'API 示例', link: '/resources/api-examples' },
            { text: '计费说明', link: '/resources/billing' },
            { text: '模型费率', link: '/resources/model-rates' },
            { text: '术语表', link: '/resources/glossary' },
            { text: '更新日志', link: '/resources/changelog' },
            { text: '安全策略', link: '/resources/security' },
            { text: '截图检查清单', link: '/resources/screenshot-checklist' }
          ]
        }
      ],
      '/support/': [
        {
          text: '帮助支持',
          collapsed: false,
          items: [
            { text: '帮助支持概览', link: '/support/' },
            { text: '常见问题', link: '/support/faq' },
            { text: '故障排查', link: '/support/troubleshooting' },
            { text: '联系支持', link: '/support/contact-support' }
          ]
        },
        {
          text: '常见错误',
          collapsed: false,
          items: [
            { text: '401 认证失败', link: '/support/401' },
            { text: '403 权限或状态限制', link: '/support/403' },
            { text: 'Base URL 错误', link: '/support/base-url-error' },
            { text: '无效 API Key', link: '/support/invalid-api-key' },
            { text: '余额不足', link: '/support/insufficient-balance' },
            { text: '模型不存在或不可用', link: '/support/model-not-found' },
            { text: 'Seedance 视频错误', link: '/support/seedance-video' },
            { text: '没有用量记录', link: '/support/no-usage-log' },
            { text: 'Windows 客户端切换', link: '/support/cc-switch-windows' }
          ]
        }
      ]
    },
    search: {
      provider: 'local',
      options: {
        translations: {
          button: {
            buttonText: '搜索文档',
            buttonAriaLabel: '搜索文档'
          },
          modal: {
            noResultsText: '没有找到相关结果',
            resetButtonTitle: '清除搜索',
            backButtonTitle: '关闭搜索',
            displayDetails: '显示详情',
            footer: {
              selectText: '选择',
              selectKeyAriaLabel: 'Enter',
              navigateText: '切换',
              navigateUpKeyAriaLabel: '向上',
              navigateDownKeyAriaLabel: '向下',
              closeText: '关闭',
              closeKeyAriaLabel: 'Esc'
            }
          }
        }
      }
    },
    outline: { label: '本页目录', level: [2, 3] },
    lastUpdated: {
      text: '最后更新',
      formatOptions: { dateStyle: 'medium', timeStyle: 'short' }
    },
    docFooter: { prev: '上一篇', next: '下一篇' },
    footer: {
      message: 'API Base URL: https://api.kkrich.ltd/v1',
      copyright: 'Copyright © KKRICH'
    }
  }
})
