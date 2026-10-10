package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/colybri/skilus/internal/app"
	"github.com/colybri/skilus/internal/domain/agent"
	"github.com/colybri/skilus/internal/domain/lock"
	"github.com/colybri/skilus/internal/domain/policy"
	"github.com/colybri/skilus/internal/domain/skill"
	"github.com/colybri/skilus/internal/domain/source"
)

type fakeSignatures struct {
	sig   app.Signature
	err   error
	calls int
}

func (f *fakeSignatures) Signature(context.Context, source.Source, string) (app.Signature, error) {
	f.calls++
	return f.sig, f.err
}

type parser struct{}

func (p parser) Package(files []skill.File) (skill.Package, error) {
	n, _ := skill.NewName("demo")
	return skill.NewPackage(n, "Demo", files)
}

func TestAuditFindings(t *testing.T) {
	f := newSyncFixture(t) // demo from github.com/o/r with an accepted run.sh
	ctx := context.Background()
	if _, err := f.disk.Put(ctx, f.pkg); err != nil {
		t.Fatal(err)
	}
	second := f.entry
	second.Skill, _ = skill.NewName("other")
	if err := f.locks.lf.Install(second); err != nil {
		t.Fatal(err)
	}
	sigs := &fakeSignatures{sig: app.Signature{State: app.Unsigned}}
	h := app.AuditHandler{
		Locks:      f.locks,
		Trust:      fakeTrust{agent.ScopeGlobal: {"github.com/anthropics"}},
		Signatures: sigs,
		Store:      f.disk,
		Trees:      f.disk,
		Parser:     parser{},
		Limits:     policy.DefaultLimits,
	}

	res, err := h.Handle(ctx, app.Audit{Scopes: project})
	if err != nil {
		t.Fatal(err)
	}
	if sigs.calls != 1 {
		t.Errorf("signature looked up %d times for one commit", sigs.calls)
	}
	a := res.Skills[0]
	codes := map[policy.Code]bool{}
	for _, x := range a.Findings {
		codes[x.Code] = true
	}
	if !codes[policy.CodeUntrusted] || !codes[policy.CodeUnsigned] || codes[policy.CodeExecutable] || !a.ContentChecked {
		t.Fatalf("findings = %+v, checked = %v", a.Findings, a.ContentChecked)
	}

	if _, err := h.Handle(ctx, app.Audit{Scopes: project, Strict: true}); !errors.Is(err, app.ErrRejected) {
		t.Fatalf("strict: err = %v", err)
	}

	// A new executable, which the lock never accepted, is reported again.
	f.locks.lf, _ = lock.Restore([]lock.Entry{withoutExecutables(f.entry)})
	sigs.sig = app.Signature{State: app.Verified, Format: "gpg", VerifiedBy: "github"}
	res, err = h.Handle(ctx, app.Audit{Scopes: project})
	if err != nil {
		t.Fatal(err)
	}
	exec := false
	for _, x := range res.Skills[0].Findings {
		if x.Code == policy.CodeUnsigned {
			t.Error("verified commit reported as unsigned")
		}
		exec = exec || x.Code == policy.CodeExecutable
	}
	if !exec {
		t.Error("an executable the lock does not accept should be reported")
	}
	if res.Skills[0].Signature.VerifiedBy != "github" {
		t.Errorf("signature = %+v", res.Skills[0].Signature)
	}
}

func withoutExecutables(e lock.Entry) lock.Entry {
	e.Executables = nil
	return e
}
