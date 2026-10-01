package notion

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"runbook/runbook"
)

func TestPageID(t *testing.T) {
	want := "1f2e3d4c-5b6a-7980-9102-3344556677ff"
	for _, in := range []string{
		"https://www.notion.so/acme/Disk-cleanup-1f2e3d4c5b6a798091023344556677ff",
		"https://notion.so/1f2e3d4c5b6a798091023344556677ff",
		"https://www.notion.so/acme/Disk-cleanup-1f2e3d4c5b6a798091023344556677ff#block2222",
		"1f2e3d4c5b6a798091023344556677ff",
		"1f2e3d4c-5b6a-7980-9102-3344556677ff",
		"  https://www.notion.so/acme/Disk-cleanup-1f2e3d4c5b6a798091023344556677ff  ",
	} {
		got, err := PageID(in)
		if err != nil || got != want {
			t.Errorf("PageID(%q) = %q, %v", in, got, err)
		}
	}
	for _, bad := range []string{
		"", "https://example.com/page-1f2e3d4c5b6a798091023344556677ff", "https://www.notion.so/acme/Tasks",
		"https://www.notion.so/acme/1f2e3d4c5b6a798091023344556677ff?v=aaaa3d4c5b6a798091023344556677ff",
	} {
		if _, err := PageID(bad); err == nil {
			t.Errorf("PageID(%q) was accepted", bad)
		}
	}
}

// fakeNotion serves one page and its blocks, in two pages of results.
func fakeNotion(t *testing.T, blocks ...map[string]any) *httptest.Server {
	t.Helper()
	page := map[string]any{
		"url":              "https://www.notion.so/acme/Disk-cleanup-1f2e",
		"last_edited_time": "2026-09-30T10:11:12.000Z",
		"properties": map[string]any{
			"Name": map[string]any{"type": "title", "title": []map[string]any{{"plain_text": "Disk cleanup"}}},
		},
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Notion-Version") != APIVersion || r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"message": "bad auth"})
			return
		}
		switch {
		case strings.HasPrefix(r.URL.Path, "/v1/pages/"):
			json.NewEncoder(w).Encode(page)
		case strings.HasSuffix(r.URL.Path, "/children"):
			// Split the blocks over two pages to exercise pagination.
			if r.URL.Query().Get("start_cursor") == "" && len(blocks) > 1 {
				json.NewEncoder(w).Encode(map[string]any{"results": blocks[:1], "has_more": true, "next_cursor": "c2"})
				return
			}
			rest := blocks
			if len(blocks) > 1 && r.URL.Query().Get("start_cursor") == "c2" {
				rest = blocks[1:]
			}
			json.NewEncoder(w).Encode(map[string]any{"results": rest, "has_more": false})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func text(s string) []map[string]any { return []map[string]any{{"plain_text": s}} }

func TestFetchPageBecomesRunbookSteps(t *testing.T) {
	srv := fakeNotion(t,
		map[string]any{"type": "paragraph", "paragraph": map[string]any{"rich_text": text("Context: run these when disks pile up.")}},
		map[string]any{"type": "heading_2", "heading_2": map[string]any{"rich_text": text("Find orphaned disks")}},
		map[string]any{"type": "code", "code": map[string]any{"language": "shell", "rich_text": text("gcloud compute disks list \\\n  --filter='-users:*'")}},
		map[string]any{"type": "heading_2", "heading_2": map[string]any{"rich_text": text("Escalate")}},
		map[string]any{"type": "paragraph", "paragraph": map[string]any{"rich_text": text("Page the storage oncall if it repeats.")}},
		map[string]any{"type": "image", "image": map[string]any{}},
		map[string]any{"type": "table", "table": map[string]any{}},
	)
	defer srv.Close()

	c := &Client{Token: "tok", BaseURL: srv.URL}
	p, err := c.FetchPage(context.Background(), "1f2e3d4c-5b6a-7980-9102-3344556677ff")
	if err != nil {
		t.Fatal(err)
	}
	if p.Title != "Disk cleanup" || p.URL == "" || p.LastEdited.IsZero() {
		t.Errorf("page meta: %+v", p)
	}
	if !reflect.DeepEqual(p.Skipped, []string{"1 image", "1 table"}) {
		t.Errorf("skipped %q", p.Skipped)
	}

	// The Markdown must parse into steps with the existing paste rules.
	res := runbook.ParsePaste(p.Markdown)
	if len(res.Steps) != 2 {
		t.Fatalf("steps: %+v\nmarkdown:\n%s", res.Steps, p.Markdown)
	}
	if s := res.Steps[0]; s.Title != "Find orphaned disks" || s.Kind != "command" ||
		s.Command != "gcloud compute disks list \\\n  --filter='-users:*'" {
		t.Errorf("step 1: %+v", s)
	}
	if s := res.Steps[1]; s.Title != "Escalate" || s.Kind != "note" || s.Note != "Page the storage oncall if it repeats." {
		t.Errorf("step 2: %+v", s)
	}
}

func TestFetchPageFollowsNestedBlocksAndInlineCode(t *testing.T) {
	srv := fakeNotion(t,
		map[string]any{"type": "heading_2", "heading_2": map[string]any{"rich_text": text("Check the node")}, "has_children": true, "id": "kid"},
		map[string]any{"type": "paragraph", "paragraph": map[string]any{"rich_text": []map[string]any{
			{"plain_text": "Look at "}, {"plain_text": "kubectl top nodes", "annotations": map[string]any{"code": true}}, {"plain_text": " first."},
		}}},
	)
	defer srv.Close()

	p, err := (&Client{Token: "tok", BaseURL: srv.URL}).FetchPage(context.Background(), "id")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.Markdown, "Look at `kubectl top nodes` first.") {
		t.Errorf("inline code lost:\n%s", p.Markdown)
	}
}

func TestFetchPageErrorsAreReadable(t *testing.T) {
	srv := fakeNotion(t)
	defer srv.Close()
	_, err := (&Client{Token: "wrong", BaseURL: srv.URL}).FetchPage(context.Background(), "id")
	if err == nil || !strings.Contains(err.Error(), "rejected the token") {
		t.Errorf("auth error: %v", err)
	}

	notShared := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"message": "Could not find page"})
	}))
	defer notShared.Close()
	_, err = (&Client{Token: "tok", BaseURL: notShared.URL}).FetchPage(context.Background(), "id")
	if err == nil || !strings.Contains(err.Error(), "Share it with your integration") {
		t.Errorf("not-shared error: %v", err)
	}
}
