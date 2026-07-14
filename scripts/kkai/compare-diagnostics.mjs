import { readFileSync } from 'node:fs'

const [kind, baselinePath, candidatePath] = process.argv.slice(2)

if (!kind || !baselinePath || !candidatePath) {
  console.error(
    'Usage: bun compare-diagnostics.mjs <go-vet|oxlint> <baseline> <candidate>'
  )
  process.exit(2)
}

function addCount(map, key) {
  map.set(key, (map.get(key) ?? 0) + 1)
}

function parseGoVet(path) {
  const counts = new Map()
  const lines = readFileSync(path, 'utf8').split(/\r?\n/)

  for (const rawLine of lines) {
    const line = rawLine.trim()
    if (!line) continue

    const match = line.match(/^(.+?):\d+(?::\d+)?:\s*(.+)$/)
    const key = match ? `${match[1]}\t${match[2]}` : `__raw__\t${line}`
    addCount(counts, key)
  }

  return counts
}

function parseOxlint(path) {
  const payload = JSON.parse(readFileSync(path, 'utf8'))
  if (!Array.isArray(payload.diagnostics)) {
    throw new Error(`Invalid oxlint JSON in ${path}`)
  }

  const counts = new Map()
  for (const diagnostic of payload.diagnostics) {
    const key = [
      diagnostic.filename,
      diagnostic.code,
      diagnostic.severity,
    ].join('\t')
    addCount(counts, key)
  }
  return counts
}

const parser = kind === 'go-vet' ? parseGoVet : kind === 'oxlint' ? parseOxlint : null
if (!parser) {
  console.error(`Unsupported diagnostic kind: ${kind}`)
  process.exit(2)
}

let baseline
let candidate
try {
  baseline = parser(baselinePath)
  candidate = parser(candidatePath)
} catch (error) {
  console.error(error instanceof Error ? error.message : String(error))
  process.exit(2)
}

const regressions = []
for (const [key, count] of candidate) {
  const allowed = baseline.get(key) ?? 0
  if (count > allowed) {
    regressions.push({ key, allowed, count })
  }
}

regressions.sort((left, right) => left.key.localeCompare(right.key))

if (regressions.length > 0) {
  console.error(`${kind}: fork introduced additional diagnostics:`)
  for (const regression of regressions) {
    console.error(
      `  ${regression.key.replaceAll('\t', ' | ')} (${regression.allowed} -> ${regression.count})`
    )
  }
  process.exit(1)
}

console.log(
  `${kind}: no diagnostic regressions (${candidate.size} candidate keys, ${baseline.size} upstream keys)`
)
