import { AnswerGate, ChooseDirectory, GetShellStatus, ResetShell, RunSteps, StopRun } from '../../wailsjs/go/main/App'
import { main } from '../../wailsjs/go/models'
import { ClipboardSetText, EventsOn } from '../../wailsjs/runtime/runtime'
import type { EndReason, Runbook } from './types'

// Frontend side of the Go run loop (app.go / package runner).

export type RunEventType =
  | 'run.start'
  | 'run.finish'
  | 'step.start'
  | 'step.output'
  | 'step.finish'
  | 'step.skip'
  | 'step.gate'

export interface RunEvent {
  type: RunEventType
  ts: string
  stepId?: string
  chunk?: string
  exit: number
  ms?: number
  reason?: EndReason
  cwd?: string
  outcome?: 'completed' | 'failed' | 'stopped' | 'error'
  message?: string
}

export type GateDecision = 'approve' | 'skip' | 'stop'
export type RunMode = 'run all' | 'run selected' | 'single'

/** Starts a run; the backend resolves each step's settings from the runbook's defaults. */
export function startRun(
  runbook: string,
  doc: Runbook,
  stepIds: string[],
  mode: RunMode,
  stepArgs: Record<string, Record<string, string>>,
): Promise<void> {
  return RunSteps(main.RunRequest.createFrom({ runbook, doc, stepIds, single: mode === 'single', mode, stepArgs }))
}

/** Resolves to false when the backend had no active run. */
export const stopRun = (): Promise<boolean> => StopRun()
export const answerGate = (d: GateDecision): Promise<void> => AnswerGate(d)

/** Subscribes to run events. Returns an unsubscribe function. */
export function onRunEvent(handler: (e: RunEvent) => void): () => void {
  return EventsOn('run', handler)
}

export interface ShellStatus {
  alive: boolean
  cwd: string
  started: string // RFC 3339; empty when not alive
}

export const resetShell = (): Promise<void> => ResetShell()
export const getShellStatus = (): Promise<ShellStatus> => GetShellStatus()
export const onShellEvent = (handler: (s: ShellStatus) => void): (() => void) => EventsOn('shell', handler)
/** Native folder picker; resolves to "" when cancelled. */
export const chooseDirectory = (current: string): Promise<string> => ChooseDirectory(current)
export const copyText = (text: string): Promise<boolean> => ClipboardSetText(text)
