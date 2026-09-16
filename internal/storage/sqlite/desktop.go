package sqlite

import (
	"context"
	"crawler/internal/ingest"
)

// DesktopCounts reads aggregate values without loading review drafts or jobs.
func (s *Store) DesktopCounts(ctx context.Context) (conspects, items int, processing string, err error) {
	processing = "idle"
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM conspects WHERE status IN ('review_pending','ready_to_apply')`).Scan(&conspects)
	if err != nil {
		return
	}
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM review_items i JOIN conspects c ON c.id=i.conspect_id WHERE c.status IN ('review_pending','ready_to_apply') AND i.status NOT IN ('resolved','applied')`).Scan(&items)
	if err != nil {
		return
	}
	rows, queryErr := s.db.QueryContext(ctx, `SELECT status,last_error FROM jobs WHERE kind IN ('extract_analysis_batch','prepare_conspect_review') AND status IN ('queued','running','retry','failed')`)
	if queryErr != nil {
		err = queryErr
		return
	}
	defer rows.Close()
	rank := 0
	for rows.Next() {
		var status, cause string
		if err = rows.Scan(&status, &cause); err != nil {
			return
		}
		next, label := 0, "idle"
		switch status {
		case "queued", "running":
			next, label = 4, "working"
		case "retry":
			next, label = 3, "retrying"
			if len(cause) >= len(ingest.ErrPaused.Error()) && cause[:len(ingest.ErrPaused.Error())] == ingest.ErrPaused.Error() {
				next, label = 2, "paused"
			}
		case "failed":
			next, label = 1, "failed"
		}
		if next > rank {
			rank = next
			processing = label
		}
	}
	err = rows.Err()
	return
}
