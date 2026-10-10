package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/colybri/skilus/internal/domain/agent"
	"github.com/colybri/skilus/internal/domain/lock"
	"github.com/colybri/skilus/internal/domain/policy"
	"github.com/colybri/skilus/internal/domain/source"
)

// Audit is the query behind `skilus audit`.
type Audit struct {
	Scopes []agent.Scope
	Strict bool // warnings make the audit fail
}

// AuditedSkill is one locked skill and what the audit found about it.
type AuditedSkill struct {
	Scope agent.Scope
	Entry lock.Entry
	// Signature is nil for sources without commits (local directories and
	// archives) and when it could not be read.
	Signature *Signature
	// ContentChecked is false when the store no longer holds the locked
	// content; skilus sync brings it back.
	ContentChecked bool
	Findings       []policy.Finding
}

// Report returns the findings as a policy report, to count them.
func (a AuditedSkill) Report() policy.Report { return policy.Report{Findings: a.Findings} }

// AuditResult lists every locked skill, in lock order.
type AuditResult struct {
	Skills []AuditedSkill
}

// AuditHandler runs Audit. It reads, and only fetches commit objects.
type AuditHandler struct {
	Locks      LockRepository
	Trust      TrustList
	Signatures SignatureChecker
	Store      Store
	Trees      TreeReader
	Parser     PackageParser
	Limits     policy.Limits
}

// Handle checks what is installed against today's rules: the trust: lists,
// the commit signatures and the inspection, which may have gained
// heuristics since the skills were added. Executables accepted when a
// skill was installed are not reported again. It returns ErrRejected with
// the full result when a finding blocks, or any does under Strict.
func (h AuditHandler) Handle(ctx context.Context, q Audit) (AuditResult, error) {
	var res AuditResult
	signatures := map[string]auditSig{} // one lookup per source and commit
	for _, scope := range q.Scopes {
		lf, err := h.Locks.Load(ctx, scope)
		if err != nil {
			return AuditResult{}, fmt.Errorf("load %s lock: %w", scope, err)
		}
		for _, e := range lf.Entries() {
			a, err := h.audit(ctx, scope, e, signatures)
			if err != nil {
				return res, fmt.Errorf("skill %s: %w", e.Skill, err)
			}
			res.Skills = append(res.Skills, a)
		}
	}

	var reasons []string
	for _, a := range res.Skills {
		r := a.Report()
		switch {
		case r.Blocking():
			reasons = append(reasons, fmt.Sprintf("%s: blocking findings", a.Entry.Skill))
		case q.Strict && r.Warnings() > 0:
			reasons = append(reasons, fmt.Sprintf("%s: %d warnings under --strict", a.Entry.Skill, r.Warnings()))
		}
	}
	if len(reasons) > 0 {
		return res, fmt.Errorf("%s: %w", strings.Join(reasons, "; "), ErrRejected)
	}
	return res, nil
}

type auditSig struct {
	sig *Signature
	err error
}

func (h AuditHandler) audit(ctx context.Context, scope agent.Scope, e lock.Entry, signatures map[string]auditSig) (AuditedSkill, error) {
	a := AuditedSkill{Scope: scope, Entry: e}
	untrusted, err := trustFindings(ctx, h.Trust, e.Source, trustScopes(scope)...)
	if err != nil {
		return a, err
	}
	a.Findings = append(a.Findings, untrusted...)

	src, err := source.Parse(e.Source)
	if err != nil {
		return a, err
	}
	switch {
	case src.Kind == source.KindLocal:
		// The user's own directory: nothing to sign.
	case e.Commit == "":
		a.Findings = append(a.Findings, policy.Finding{Code: policy.CodeUnsigned, Severity: policy.Warn, Detail: "archives carry no signature; the lock pins their content by hash"})
	case h.Signatures != nil:
		key := src.ID + "@" + e.Commit
		s, ok := signatures[key]
		if !ok {
			sig, err := h.Signatures.Signature(ctx, src, e.Commit)
			s = auditSig{err: err}
			if err == nil {
				s.sig = &sig
			}
			signatures[key] = s
		}
		a.Signature = s.sig
		switch {
		case s.err != nil:
			a.Findings = append(a.Findings, policy.Finding{Code: policy.CodeUnchecked, Severity: policy.Warn, Detail: "could not read the commit signature: " + s.err.Error()})
		case s.sig.State == Unsigned:
			a.Findings = append(a.Findings, policy.Finding{Code: policy.CodeUnsigned, Severity: policy.Warn, Detail: "commit " + shortCommit(e.Commit) + " is not signed"})
		}
	}

	findings, checked := h.inspect(ctx, e)
	a.ContentChecked = checked
	a.Findings = append(a.Findings, findings...)
	return a, nil
}

// inspect runs today's inspection on the locked content in the store.
func (h AuditHandler) inspect(ctx context.Context, e lock.Entry) ([]policy.Finding, bool) {
	if h.Store == nil || h.Parser == nil {
		return nil, false
	}
	dir, ok, err := h.Store.Lookup(ctx, e.TreeHash)
	if err != nil || !ok {
		return nil, false
	}
	files, got, err := readInstalled(ctx, h.Trees, dir, e)
	if err != nil || got != e.TreeHash {
		return nil, false
	}
	p, err := h.Parser.Package(files)
	if err != nil {
		return []policy.Finding{{Code: policy.CodeInvalidSkill, Severity: policy.Block, Detail: "SKILL.md no longer passes validation: " + err.Error()}}, true
	}
	r := policy.Inspect(p, h.Limits, policy.Allow{})
	accepted := map[string]bool{}
	for _, x := range e.Executables {
		accepted[x] = true
	}
	var out []policy.Finding
	for _, f := range r.Findings {
		if f.Code == policy.CodeExecutable && accepted[f.Path] {
			continue
		}
		out = append(out, f)
	}
	return out, true
}

func shortCommit(c string) string {
	if len(c) > 12 {
		return c[:12]
	}
	return c
}
