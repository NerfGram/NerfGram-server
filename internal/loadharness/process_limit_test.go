package loadharness

import "testing"

func TestMinimumOpenFilesHasFixedAndPerSessionHeadroom(t *testing.T) {
	if got, want := minimumOpenFiles(0), 256; got != want {
		t.Fatalf("minimumOpenFiles(0) = %d, want %d", got, want)
	}
	if got, want := minimumOpenFiles(500), 3256; got != want {
		t.Fatalf("minimumOpenFiles(500) = %d, want %d", got, want)
	}
}
