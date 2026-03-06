package gamecommon

import "testing"

func TestStatusConstants(t *testing.T) {
	if StatusLobby != "lobby" {
		t.Errorf("StatusLobby = %q, want lobby", StatusLobby)
	}
	if StatusInProgress != "in_progress" {
		t.Errorf("StatusInProgress = %q, want in_progress", StatusInProgress)
	}
	if StatusFinished != "finished" {
		t.Errorf("StatusFinished = %q, want finished", StatusFinished)
	}
}
