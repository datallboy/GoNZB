package wiring

import "testing"

func TestUploaderActivePublicationLink(t *testing.T) {
	t.Run("same release restores prior lifecycle", func(t *testing.T) {
		supersedes, reason := uploaderActivePublicationLink("rel_same", "rel_same", "evt_withdrawn")
		if supersedes != "evt_withdrawn" || reason != "explicit uploader restoration" {
			t.Fatalf("unexpected restoration link: supersedes=%q reason=%q", supersedes, reason)
		}
	})

	t.Run("corrected release starts a new lifecycle", func(t *testing.T) {
		supersedes, reason := uploaderActivePublicationLink("rel_old", "rel_corrected", "evt_withdrawn")
		if supersedes != "" || reason != "corrected uploader publication" {
			t.Fatalf("unexpected corrected publication link: supersedes=%q reason=%q", supersedes, reason)
		}
	})
}
