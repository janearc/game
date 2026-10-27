package stack_test

import (
	"testing"

	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/stack"
)

func TestMemberKeepsItsRequestedReference(t *testing.T) {
	t.Parallel()
	member := stack.Named("library", "v1.2.3")
	if member.Name() != "library" || member.Ref() != "v1.2.3" {
		t.Fatalf("member lost its requested identity: %+v", member)
	}
}

func TestMemberDoesNotInventAReference(t *testing.T) {
	t.Parallel()
	if member := stack.Named("library", ""); member.Ref() != "" {
		t.Fatalf("unspecified reference became %q", member.Ref())
	}
}

func TestEventKeepsEverySuppliedField(t *testing.T) {
	t.Parallel()
	event := stack.Happened("lint", "fail", "deterministic", "findings", count.One)
	if event.Step != "lint" || event.State != "fail" || event.Kind != "deterministic" ||
		event.Text != "findings" || event.Count != count.One || event.Repo != "" {
		t.Fatalf("event changed supplied fields: %+v", event)
	}
}

func TestEventDoesNotInventOptionalFields(t *testing.T) {
	t.Parallel()
	event := stack.Happened("check", "ok", "", "", count.Nothing)
	if event.Kind != "" || event.Text != "" || event.Count != count.Nothing ||
		event.Repo != "" {
		t.Fatalf("event invented optional fields: %+v", event)
	}
}
