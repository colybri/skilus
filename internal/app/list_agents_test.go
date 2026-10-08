package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/colybri/skilus/internal/app"
	"github.com/colybri/skilus/internal/domain/agent"
)

type fakeCatalog struct {
	agents []agent.Agent
	err    error
}

func (f fakeCatalog) Agents(context.Context) ([]agent.Agent, error) { return f.agents, f.err }

type fakeDetector map[string]bool

func (f fakeDetector) Installed(_ context.Context, a agent.Agent) (bool, error) {
	return f[a.ID().String()], nil
}

func newAgent(t *testing.T, id string) agent.Agent {
	t.Helper()
	aid, err := agent.NewID(id)
	if err != nil {
		t.Fatal(err)
	}
	a, err := agent.New(aid, id, ".agents/skills", "/home/u/."+id+"/skills", "/home/u/."+id)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestListAgentsKeepsCatalogOrderAndDetection(t *testing.T) {
	q := app.ListAgents{
		Catalog:  fakeCatalog{agents: []agent.Agent{newAgent(t, "codex"), newAgent(t, "cursor")}},
		Detector: fakeDetector{"cursor": true},
	}
	got, err := q.Handle(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Agent.ID().String() != "codex" || got[0].Installed || !got[1].Installed {
		t.Fatalf("Handle() = %+v", got)
	}
}

func TestListAgentsWrapsCatalogError(t *testing.T) {
	boom := errors.New("boom")
	_, err := app.ListAgents{Catalog: fakeCatalog{err: boom}, Detector: fakeDetector{}}.Handle(context.Background())
	if !errors.Is(err, boom) {
		t.Fatalf("error = %v, want wrapped boom", err)
	}
}
