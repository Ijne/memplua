package processing

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"crawler/internal/ingest"
	"crawler/internal/knowledge"
)

func TestAutomaticExportHonorsConfigurationAndFansOutDeterministically(t *testing.T) {
	repository := staticSnapshotRepository{snapshot: knowledge.Snapshot{Revision: 9}}
	var calls []string
	exporters := map[string]knowledge.Exporter{
		"zeta":  recordingExporter{name: "zeta", calls: &calls},
		"alpha": recordingExporter{name: "alpha", calls: &calls},
	}
	payload, _ := json.Marshal(ingest.ExportPayload{Automatic: true})
	job := ingest.Job{Kind: ingest.JobExport, Payload: payload}
	if err := (ExportHandler{Repository: repository, Exporters: exporters, Auto: false}).Handle(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 0 {
		t.Fatalf("disabled automatic export called %v", calls)
	}
	if err := (ExportHandler{Repository: repository, Exporters: exporters, Auto: true}).Handle(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(calls, []string{"alpha", "zeta"}) {
		t.Fatalf("export calls = %v", calls)
	}
}

func TestManualExportSelectsOneExporterEvenWhenAutoIsDisabled(t *testing.T) {
	repository := staticSnapshotRepository{}
	var calls []string
	payload, _ := json.Marshal(ingest.ExportPayload{Exporter: "obsidian"})
	handler := ExportHandler{
		Repository: repository,
		Exporters: map[string]knowledge.Exporter{
			"obsidian": recordingExporter{name: "obsidian", calls: &calls},
			"other":    recordingExporter{name: "other", calls: &calls},
		},
	}
	if err := handler.Handle(context.Background(), ingest.Job{Kind: ingest.JobExport, Payload: payload}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(calls, []string{"obsidian"}) {
		t.Fatalf("manual export calls = %v", calls)
	}
}

type staticSnapshotRepository struct {
	snapshot knowledge.Snapshot
}

func (r staticSnapshotRepository) Snapshot(context.Context) (knowledge.Snapshot, error) {
	return r.snapshot, nil
}

type recordingExporter struct {
	name  string
	calls *[]string
}

func (e recordingExporter) Name() string { return e.name }
func (e recordingExporter) Export(_ context.Context, _ knowledge.Snapshot) error {
	*e.calls = append(*e.calls, e.name)
	return nil
}
