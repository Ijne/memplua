// Package app owns the process lifecycle and concurrent source sessions.
// Application supervises long-running runners under one cancellation context,
// while SourceManager starts, pauses, resumes, and stops independently named
// sources without knowing their native implementation.
package app
