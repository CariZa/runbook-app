// Mirrors package runbook (Go). Optional step fields are overrides; undefined = inherit.

export type StepKind = 'command' | 'note'

export interface Step {
  id: string
  title: string
  kind: StepKind
  command: string // command steps only; may be multi-line
  note: string // note steps only
  destructive?: boolean
  continueOnFail?: boolean
  skipInRunAll?: boolean
  cwd?: string // path; undefined/"" = inherit
  timeoutSec?: number // 0 = no timeout
  recordOutput?: boolean // false = never write this step's output to disk
  extra?: string // other Markdown under the step, kept verbatim
}

export type SettingField = 'destructive' | 'continueOnFail' | 'skipInRunAll' | 'recordOutput' | 'cwd' | 'timeoutSec'

export interface Runbook {
  name: string
  cwd: string
  version: number
  parent: string
  defaults: { destructive: boolean; continueOnFail: boolean; timeoutSec: number }
  running: { pauseAtDestructive: boolean; audit: boolean }
  notion: { pageId: string; url: string }
  steps: Step[]
  preamble: string
  frontMatterRaw: string
}

// What the runbook settings dialog (5c) returns.
export interface SettingsResult {
  name: string
  cwd: string
  defaults: Runbook['defaults']
  running: Runbook['running']
  resetOverrides: boolean
  folder: string // "" = top level
}

export interface Summary {
  name: string // ref: "name" or "folder/name"
  folder: string // "" at the top level
  steps: number
  modified: string
  lastRun: string
}

export interface Version {
  n: number
  saved: string
  parent: string
}

// Retained last output of a step (from .runbook/state.json), already redacted.
export interface SavedOutput {
  status: 'passed' | 'failed'
  output: string
  exit: number
  ms: number
  startedAt: string
  reason: EndReason
  command: string
  cwd: string
}

export type RunStatus = 'running' | 'passed' | 'failed'
export type EndReason = 'exit' | 'stopped' | 'timeout' | 'shell-exited' | 'error'

// Last run of one step as the UI shows it (SPEC.md §2 "Output").
export interface StepRun {
  status: RunStatus
  output: string
  startedAt: number // epoch ms
  exitCode?: number
  durationMs?: number
  endReason?: EndReason
  command?: string // as it ran
  cwd?: string // where it ran
}

// One line of runs.log (without output).
export interface AuditEntry {
  ts?: string
  event: string
  runId?: string
  stepId?: string
  step?: number
  title?: string
  command?: string
  cwd?: string
  exit?: number
  ms?: number
  reason?: string
  outcome?: string
  outputRecorded?: boolean
  before?: { title: string; body: string }
  after?: { title: string; body: string }
  message?: string
}
