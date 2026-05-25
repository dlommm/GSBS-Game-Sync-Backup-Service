package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWatchPathDiff(t *testing.T) {
	old := []watchPath{
		{GameID: "1", PathKey: "a", RuleKey: "a"},
		{GameID: "2", PathKey: "b", RuleKey: "b"},
	}
	newWP := []watchPath{
		{GameID: "1", PathKey: "a", RuleKey: "a"},
		{GameID: "3", PathKey: "c", RuleKey: "c"},
	}
	added, removed := watchPathDiff(old, newWP)
	assert.Equal(t, 1, added)
	assert.Equal(t, 1, removed)
}

func TestWatchPathDiff_NoChange(t *testing.T) {
	wp := []watchPath{{GameID: "1", PathKey: "k", RuleKey: "k"}}
	added, removed := watchPathDiff(wp, wp)
	assert.Equal(t, 0, added)
	assert.Equal(t, 0, removed)
}

func TestWatchPathDiff_RebuildAfterDirAppears(t *testing.T) {
	// Simulates discovery periodic rebuild: same game set, new rule added when dir exists.
	before := []watchPath{{GameID: "100", PathKey: "r1", RuleKey: "r1"}}
	after := []watchPath{
		{GameID: "100", PathKey: "r1", RuleKey: "r1"},
		{GameID: "100", PathKey: "r2", RuleKey: "r2"},
	}
	added, removed := watchPathDiff(before, after)
	assert.Equal(t, 1, added)
	assert.Equal(t, 0, removed)
}
