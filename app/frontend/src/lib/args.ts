import type { Step } from './types'

// {{name}} blanks in commands. Mirrors runbook/args.go, which does the real substitution
// when a step runs; this copy only drives what the UI shows before you press ▶.

const ARG = /\{\{\s*([A-Za-z0-9_-]+)\s*\}\}/g
const SAFE = /^[A-Za-z0-9_./:=@%+,-]+$/

export function argNames(command: string): string[] {
  return [...new Set([...command.matchAll(ARG)].map((m) => m[1]))]
}

export function quoteArg(v: string): string {
  return SAFE.test(v) ? v : `'${v.replaceAll("'", `'\\''`)}'`
}

/** Names of this step's blanks that have no value yet. */
export function missingArgs(step: Step, values: Record<string, string> | undefined): string[] {
  if (step.kind !== 'command') return []
  return argNames(step.command).filter((n) => !(values?.[n] ?? '').trim())
}

/** The command exactly as it will run, or null while any blank is empty. */
export function resolveCommand(command: string, values: Record<string, string>): string | null {
  let complete = true
  const out = command.replace(ARG, (m, name: string) => {
    const v = (values[name] ?? '').trim()
    if (!v) complete = false
    return v ? quoteArg(v) : m
  })
  return complete ? out : null
}

export type Segment = { text: string } | { arg: string; value: string | null }

/** Splits a command into literal text and filled/empty blanks, for display. */
export function segments(command: string, values: Record<string, string>): Segment[] {
  const out: Segment[] = []
  let last = 0
  for (const m of command.matchAll(ARG)) {
    if (m.index! > last) out.push({ text: command.slice(last, m.index) })
    const v = (values[m[1]] ?? '').trim()
    out.push({ arg: m[1], value: v ? quoteArg(v) : null })
    last = m.index! + m[0].length
  }
  if (last < command.length) out.push({ text: command.slice(last) })
  return out
}
