import path from 'path';
import { createRequire } from 'module';
import { fileURLToPath } from 'url';
import { defineConfig, loadEnv } from '@rsbuild/core';
import { pluginReact } from '@rsbuild/plugin-react';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const require = createRequire(import.meta.url);
const semiUiDir = path.resolve(
  path.dirname(require.resolve('@douyinfe/semi-ui')),
  '../..',
);
const dateFnsDir = path.dirname(require.resolve('date-fns/package.json'));

export default defineConfig(({ envMode }) => {
  const env = loadEnv({ mode: envMode, prefixes: ['VITE_'] });
  const forceRelativeApi = process.env.KKAI_EXTERNAL_FRONTEND_BUILD === '1';
  const clientServerUrl = forceRelativeApi
    ? ''
    : process.env.VITE_REACT_APP_SERVER_URL ||
      env.rawPublicVars.VITE_REACT_APP_SERVER_URL ||
      '';
  const proxyServerUrl = clientServerUrl || 'http://localhost:3000';
  const invitationsApiUrl =
    process.env.VITE_INVITATIONS_API_URL ||
    env.rawPublicVars.VITE_INVITATIONS_API_URL ||
    'http://localhost:6212';
  const isProd = envMode === 'production';
  const devProxy = Object.fromEntries(
    (
      [
        '/api',
        '/invitations/api',
        '/mj',
        '/pg',
        '/v1',
        '/v1beta',
        '/suno',
        '/kling',
        '/jimeng',
      ] as const
    ).map((key) => [
      key,
      {
        target: key === '/invitations/api' ? invitationsApiUrl : proxyServerUrl,
        changeOrigin: true,
        ...(key === '/invitations/api'
          ? { pathRewrite: { '^/invitations/api': '/api' } }
          : {}),
      },
    ]),
  ) as Record<
    string,
    {
      target: string;
      changeOrigin: boolean;
      pathRewrite?: Record<string, string>;
    }
  >;

  return {
    plugins: [pluginReact()],
    source: {
      entry: {
        index: './src/index.jsx',
      },
      define: {
        'import.meta.env.VITE_REACT_APP_SERVER_URL':
          JSON.stringify(clientServerUrl),
      },
    },
    resolve: {
      alias: {
        '@': path.resolve(__dirname, './src'),
        '@douyinfe/semi-ui/dist/css/semi.css': path.resolve(
          semiUiDir,
          'dist/css/semi.css',
        ),
        'date-fns': dateFnsDir,
      },
    },
    html: {
      template: './index.html',
    },
    server: {
      host: '0.0.0.0',
      strictPort: false,
      proxy: devProxy,
    },
    output: {
      minify: isProd,
      target: 'web',
      distPath: {
        root: 'dist',
      },
    },
    performance: {
      removeConsole: isProd ? ['log'] : false,
      buildCache: {
        cacheDigest: [process.env.VITE_REACT_APP_VERSION],
      },
    },
    tools: {
      rspack: {
        module: {
          rules: [
            {
              test: /src[\\/].*\.js$/,
              type: 'javascript/auto',
              use: [
                {
                  loader: 'builtin:swc-loader',
                  options: {
                    jsc: {
                      parser: {
                        syntax: 'ecmascript',
                        jsx: true,
                      },
                      transform: {
                        react: {
                          runtime: 'automatic',
                          development: !isProd,
                          refresh: !isProd,
                        },
                      },
                    },
                  },
                },
              ],
            },
          ],
        },
      },
    },
  };
});
