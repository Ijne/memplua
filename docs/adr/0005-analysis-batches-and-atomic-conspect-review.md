# Analysis batches and atomic Conspect review

Status: accepted. Supersedes ADR 0002 and the database filename in ADR 0001.

Persist every text chunk together with its unique FOCUS assignment. Collect for at most five minutes or ninety seconds of inactivity; Finalize and source stop close immediately. Store a bounded Unicode context tail from the immediately preceding batch. Closure and its extraction job share one transaction.

One extraction response supplies topic groups, local term refs, evidence IDs and FOCUS anchors. Validate before persisting candidates; retain exact duplicate evidence and semantic variants. Empty/context-only candidates never reach review. Raw/normalized responses, model and prompt version belong to AnalysisBatch. Network retries cannot guarantee exactly-once external inference; persisted results and topic creation are idempotent.

Review preparation uses Unicode normalization, taxonomy aliases, tokens and character trigrams, with no LLM merge. Shortlists are suggestions only. Incoming variants can be split; target selection outside the shortlist uses canonical graph search. Decisions update review state only.

Apply Conspect checks all target versions and commits entities, explicit taxonomy resolutions, links, provenance, revisions, one graph revision, an export job and a durable event in one SQLite transaction. A conflict rolls back and queues rematching. Repeated Apply is a no-op. An entirely rejected conspect becomes rejected without a graph revision or export. ThoughtTerm is never a user review item.

Use a fresh storage.database, default knowledge.db. Do not import, delete or modify knowledgecrawler.db, WAL or SHM. Production jobs are extract_analysis_batch, prepare_conspect_review and export. SSE reads the durable event outbox, including events written inside transactions.
