export type EnvironmentRow = { key: string; value: string }

/** A variable with the comment and blank lines written above it. */
export type EnvironmentLine = EnvironmentRow & { before: string[] }

/**
 * .env text as variables in the order written. Comment ("# …") and blank lines are kept
 * with the variable under them; the ones after the last variable are the tail.
 */
export function parseEnvironmentText(text: string): { rows: EnvironmentLine[]; tail: string[] } {
  const rows: EnvironmentLine[] = []
  let notes: string[] = []
  text.split(/\r?\n/).forEach((line, index) => {
    const trimmed = line.trim()
    if (!trimmed || trimmed.startsWith('#')) { notes.push(trimmed); return }
    const match = line.match(/^\s*(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=(.*)$/)
    if (!match) throw new Error(String(index + 1))
    let value = match[2]
    if (value.startsWith('"')) {
      try { value = JSON.parse(value) as string } catch { throw new Error(String(index + 1)) }
    } else if (value.startsWith("'")) {
      if (!value.endsWith("'") || value.length < 2) throw new Error(String(index + 1))
      value = value.slice(1, -1)
    }
    rows.push({ key: match[1], value, before: notes })
    notes = []
  })
  while (notes.length && !notes[notes.length - 1]) notes.pop() // blank lines at the very end mean nothing
  return { rows, tail: notes }
}

export function parseEnvironment(text: string): EnvironmentRow[] {
  return parseEnvironmentText(text).rows.map(({ key, value }) => ({ key, value }))
}

/** KEY=value as the parser reads it back: bare when the value survives that, quoted otherwise. */
function assignment(key: string, value: string): string {
  const bare = value === '' || (/^[^\s"'].*$/.test(value) && !/\s$/.test(value) && !/[\r\n]/.test(value))
  return `${key}=${bare ? value : JSON.stringify(value)}`
}

export function serializeEnvironment(rows: EnvironmentRow[]): string {
  return rows.map(({ key, value }) => assignment(key, value)).join('\n')
}

/** The text parseEnvironmentText reads back to the same rows and tail. */
export function serializeEnvironmentText(rows: EnvironmentLine[], tail: string[]): string {
  return [...rows.flatMap(({ key, value, before }) => [...before, assignment(key, value)]), ...tail].join('\n')
}

export function environmentMap(rows: EnvironmentRow[]): Record<string, string> {
  const result: Record<string, string> = Object.create(null)
  for (const { key, value } of rows) {
    if (!/^[A-Za-z_][A-Za-z0-9_]*$/.test(key) || key.length > 256 || key in result || value.includes('\0')) throw new Error(key)
    result[key] = value
  }
  return result
}
