// Shared error formatter for UI toast / message surfaces. Each panel used
// to ship its own copy of this two-line helper; centralizing keeps
// formatting consistent (one place to attach stack-trace links, redact
// PII, swap the surface text, etc.).
export function toErrorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}
