import { spawnSync } from 'node:child_process'
import {
  copyFileSync,
  existsSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from 'node:fs'
import { tmpdir } from 'node:os'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '../..')
const baseline = process.argv[2]

if (!baseline) {
  console.error('Usage: bun check-changed-format.mjs <upstream-commit>')
  process.exit(2)
}

function run(command, args, options = {}) {
  const result = spawnSync(command, args, {
    cwd: root,
    encoding: 'utf8',
    maxBuffer: 64 * 1024 * 1024,
    ...options,
  })
  if (result.error) throw result.error
  return result
}

const diff = run('git', [
  'diff',
  '--name-only',
  '--diff-filter=ACMR',
  '-z',
  baseline,
  '--',
  'web',
])

if (diff.status !== 0) {
  process.stderr.write(diff.stderr)
  process.exit(diff.status ?? 1)
}

const changedFiles = diff.stdout.split('\0').filter(Boolean)
const supportedExtensions = new Set([
  '.cjs',
  '.css',
  '.html',
  '.js',
  '.json',
  '.jsonc',
  '.jsx',
  '.md',
  '.mjs',
  '.scss',
  '.ts',
  '.tsx',
  '.yaml',
  '.yml',
])
const headerPattern =
  /^\/\*\nCopyright \(C\)[\s\S]*?QuantumNous[\s\S]*?\*\/\n+/
const frontendFiles = changedFiles.filter((path) => {
  if (!path.startsWith('web/')) return false
  if (!existsSync(join(root, path))) return false
  const dot = path.lastIndexOf('.')
  return dot !== -1 && supportedExtensions.has(path.slice(dot))
})

if (frontendFiles.length > 0) {
  const temporaryRoot = mkdtempSync(join(tmpdir(), 'kkai-oxfmt-'))
  try {
    const expected = new Map()
    for (const path of frontendFiles) {
      const relativePath = path.slice('web/'.length)
      const sourcePath = join(root, path)
      const targetPath = join(temporaryRoot, relativePath)
      mkdirSync(dirname(targetPath), { recursive: true })

      const content = readFileSync(sourcePath, 'utf8')
      const stripped = content.replace(headerPattern, '')
      expected.set(relativePath, stripped)
      writeFileSync(targetPath, stripped)
    }

    const configPath = join(root, 'web/.oxfmtrc.json')
    copyFileSync(configPath, join(temporaryRoot, '.oxfmtrc.json'))
    const oxfmt = join(root, 'web/node_modules/.bin/oxfmt')
    const result = run(oxfmt, ['-c', '.oxfmtrc.json', '--write', '.'], {
      cwd: temporaryRoot,
    })
    process.stdout.write(result.stdout)
    process.stderr.write(result.stderr)
    if (result.status !== 0) process.exit(result.status ?? 1)

    const issues = []
    for (const [relativePath, original] of expected) {
      const formatted = readFileSync(join(temporaryRoot, relativePath), 'utf8')
      if (formatted !== original) issues.push(relativePath)
    }

    if (issues.length > 0) {
      console.error('Frontend format issues in fork-owned changes:')
      for (const path of issues) console.error(`  web/${path}`)
      process.exit(1)
    }
  } finally {
    rmSync(temporaryRoot, { recursive: true, force: true })
  }
}

console.log(`format: checked ${frontendFiles.length} frontend changed files`)
