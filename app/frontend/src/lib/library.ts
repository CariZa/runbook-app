import {
  CreateFolder,
  CreateFromNotion,
  CreateRunbook,
  FetchNotionPage,
  ForgetNotionToken,
  HasNotionToken,
  NotionTokenHint,
  OpenInBrowser,
  SaveNotionToken,
  DeleteFolder,
  MoveFolder,
  MoveRunbook,
  RenameFolder,
  DeleteRunbook,
  ExportMarkdown,
  ParsePaste,
  SaveExport,
  ForkRunbook,
  ListRunbooks,
  LogEvent,
  OpenAuditLog,
  OpenRunbook,
  RenameRunbook,
  SaveStepArgs,
  SaveRunbook,
  SaveVersion,
} from '../../wailsjs/go/main/App'
import type { AuditEntry, Runbook, SavedOutput, Step, Summary, Version } from './types'

// Frontend side of the runbook library (library.go / package runbook).
// The generated bindings use classes; these plain shapes are what actually crosses the bridge.

export interface Opened {
  doc: Runbook
  outputs: Record<string, SavedOutput>
  versions: Version[] | null
  stepArgs: Record<string, Record<string, string>> | null
}

const asDoc = (d: Runbook) => d as any

export const listRunbooks = async (): Promise<{ runbooks: Summary[]; folders: string[] }> => {
  const lib = (await ListRunbooks()) as any
  return { runbooks: lib?.runbooks ?? [], folders: lib?.folders ?? [] }
}
/** ref is "name" or "parent/name". */
export const createFolder = (ref: string): Promise<void> => CreateFolder(ref)
/** Moves a folder into parent ("" = top level); resolves to its new ref. */
export const moveFolder = (ref: string, parent: string): Promise<string> => MoveFolder(ref, parent)
/** Renames a folder in place; resolves to its new ref. */
export const renameFolder = (ref: string, name: string): Promise<string> => RenameFolder(ref, name)
/** Moves the folder and its runbooks to the Trash; resolves to where it went. */
export const deleteFolder = (name: string): Promise<string> => DeleteFolder(name)
/** Moves a runbook into folder ("" = top level); resolves to its new ref. */
export const moveRunbook = (ref: string, folder: string): Promise<string> => MoveRunbook(ref, folder)
export const openRunbook = async (name: string): Promise<Opened> => (await OpenRunbook(name)) as any
export const createRunbook = async (name: string): Promise<Runbook> => (await CreateRunbook(name)) as any
export const saveRunbook = (name: string, doc: Runbook): Promise<void> => SaveRunbook(name, asDoc(doc))
/** Renames within the runbook's folder; resolves to the new ref. */
export const renameRunbook = (old: string, doc: Runbook, next: string): Promise<string> =>
  RenameRunbook(old, asDoc(doc), next)
export const saveVersion = async (name: string, doc: Runbook): Promise<{ doc: Runbook; versions: Version[] }> =>
  (await SaveVersion(name, asDoc(doc))) as any
export const forkRunbook = async (
  src: string,
  doc: Runbook,
  stepIds: string[],
  newName: string,
  keepOutputs: boolean,
  linkParent: boolean,
): Promise<Runbook> => (await ForkRunbook(src, asDoc(doc), stepIds, newName, keepOutputs, linkParent)) as any
/** Moves the runbook to the Trash; resolves to where it went. */
export const deleteRunbook = (name: string): Promise<string> => DeleteRunbook(name)
export const saveStepArgs = (name: string, stepId: string, args: Record<string, string>): Promise<void> =>
  SaveStepArgs(name, stepId, args)
export const logEvent = (name: string, e: AuditEntry): Promise<void> => LogEvent(name, e as any)

/** Opens runs.log in the default text editor, or reveals it in the Finder. */
export const openAuditLog = (name: string, reveal: boolean): Promise<void> => OpenAuditLog(name, reveal)

export interface ExportOptions {
  stepIds: string[] // empty = every step
  outputs: boolean
  settings: boolean
  timestamps: boolean
}

export interface PasteResult {
  steps: Step[] | null
  dropped: string[] | null
}

export const exportMarkdown = (name: string, doc: Runbook, opts: ExportOptions): Promise<string> =>
  ExportMarkdown(name, asDoc(doc), opts as any)
/** Resolves to the saved path, or "" if the user cancelled the save dialog. */
export const saveExport = (name: string, markdown: string, audit: boolean): Promise<string> => SaveExport(name, markdown, audit)
export const parsePaste = async (text: string): Promise<PasteResult> => (await ParsePaste(text)) as any

// --- Notion (read-only import) ---

export interface NotionPage {
  pageId: string
  title: string
  url: string
  markdown: string
  skipped: string[] | null
  steps: Step[] | null
  dropped: string[] | null
  suggest: string
}

export const hasNotionToken = (): Promise<boolean> => HasNotionToken()
/** The saved token, masked for display ("" when none is saved). */
export const notionTokenHint = (): Promise<string> => NotionTokenHint()
export const saveNotionToken = (token: string): Promise<void> => SaveNotionToken(token)
export const forgetNotionToken = (): Promise<void> => ForgetNotionToken()
export const fetchNotionPage = async (link: string): Promise<NotionPage> => (await FetchNotionPage(link)) as any
export const createFromNotion = async (ref: string, steps: Step[], pageId: string, pageUrl: string): Promise<Runbook> =>
  (await CreateFromNotion(ref, steps as any, pageId, pageUrl)) as any
export const openInBrowser = (link: string): Promise<void> => OpenInBrowser(link)
