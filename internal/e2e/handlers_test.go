package e2e

import (
	"path/filepath"
	"testing"
)

// The code of an update handler and of an update validator is mapped like a
// small workflow, recorded when a test sends the update, and listed in the
// handler table.
func TestUpdateHandlersAndValidatorsAreMappedAndRecorded(t *testing.T) {
	fixtures := map[string]string{}
	for _, dir := range []string{"update_handler", "update_validator", "update_named"} {
		fixtures[dir] = abs(t, "../../testdata/constructs/"+dir)
	}
	for _, dir := range []string{"update_handler", "update_validator", "update_named"} {
		fixture := fixtures[dir]
		t.Chdir(t.TempDir())
		traces := filepath.Join(t.TempDir(), "traces")
		if _, stderr, code := pathkit(t, "test", fixture+"/...", "--traces", traces); code != 0 {
			t.Fatalf("%s: pathkit test: exit %d\n%s", dir, code, stderr)
		}
		doc := summarizeReport(t, fixture, traces, "")
		handlers, _ := doc["handlers"].([]any)
		var update, validator map[string]any
		for _, h := range handlers {
			m := h.(map[string]any)
			switch m["kind"] {
			case "update":
				update = m
			case "update validator":
				validator = m
			}
		}
		if update == nil {
			t.Fatalf("%s: no update handler in the table: %v", dir, handlers)
		}
		if update["name"] != "add" || update["exercised"] != "yes" || update["handlerWorkflow"] != dir+".Run.update[add]" {
			t.Errorf("%s: update row: %v", dir, update)
		}
		if tr := doc["traces"].(map[string]any); tr["unmatched"].(float64) != 0 {
			t.Errorf("%s: traces: %v", dir, tr)
		}
		switch dir {
		case "update_handler":
			w := workflowNamed(t, doc, "update_handler.Run.update[add]")["paths"].(map[string]any)
			if w["total"].(float64) != 3 || w["covered"].(float64) != 2 {
				t.Errorf("handler paths: %v", w)
			}
		case "update_named":
			// The test sends a refused update: one of the two paths of the method.
			w := workflowNamed(t, doc, "update_named.Run.update[add]")["paths"].(map[string]any)
			if w["total"].(float64) != 2 || w["covered"].(float64) != 1 {
				t.Errorf("named handler paths: %v", w)
			}
		case "update_validator":
			if validator == nil || validator["exercised"] != "yes" {
				t.Fatalf("validator row: %v", validator)
			}
			// The test sends an accepted update only: one of the two paths.
			w := workflowNamed(t, doc, "update_validator.Run.validator[add]")["paths"].(map[string]any)
			if w["total"].(float64) != 2 || w["covered"].(float64) != 1 {
				t.Errorf("validator paths: %v", w)
			}
			b := validator["branches"].(map[string]any)
			if b["total"].(float64) != 2 || b["covered"].(float64) != 1 {
				t.Errorf("validator branches: %v", b)
			}
		}
	}
}
