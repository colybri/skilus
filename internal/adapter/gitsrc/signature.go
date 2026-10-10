package gitsrc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/colybri/skilus/internal/app"
	"github.com/colybri/skilus/internal/domain"
	"github.com/colybri/skilus/internal/domain/source"
)

var _ app.SignatureChecker = Signatures{}

// Signatures implements app.SignatureChecker. It fetches the commit object
// alone and verifies its signature with git verify-commit, so the user's
// GPG keyring, SSH allowed signers and gpg.x509.program (gitsign, for
// Sigstore) apply. When git cannot verify a signed commit of a GitHub
// repository, it asks GitHub, which knows the keys its users registered.
type Signatures struct {
	Fetcher Fetcher
	// GitHubAPI is the REST API base; empty means https://api.github.com.
	// "-" turns the GitHub check off.
	GitHubAPI string
	// Token, usually $GITHUB_TOKEN, raises GitHub's rate limit. It is
	// only sent to the GitHub API and never stored.
	Token  string
	Client *http.Client
}

// Signature implements app.SignatureChecker.
func (s Signatures) Signature(ctx context.Context, src source.Source, commit string) (app.Signature, error) {
	if !isCommit(commit) {
		return app.Signature{}, fmt.Errorf("%q is not a commit id: %w", commit, domain.ErrInvalid)
	}
	f := s.Fetcher
	dir, err := os.MkdirTemp("", "skilus-sig-")
	if err != nil {
		return app.Signature{}, err
	}
	defer os.RemoveAll(dir) //nolint:errcheck // temporary repository

	if _, err := f.git(ctx, dir, nil, "init", "--quiet", "--bare", dir); err != nil {
		return app.Signature{}, err
	}
	if _, err := f.git(ctx, dir, nil, "fetch", "--quiet", "--depth=1", "--filter=blob:none", "--no-tags", "--end-of-options", src.URL, commit); err != nil {
		if notFound(err) {
			return app.Signature{}, fmt.Errorf("fetch %s: %w: %w", src, err, domain.ErrNotFound)
		}
		return app.Signature{}, fmt.Errorf("fetch %s: %w", src, err)
	}
	raw, err := f.git(ctx, dir, nil, "cat-file", "commit", commit)
	if err != nil {
		return app.Signature{}, err
	}
	format := signatureFormat(string(raw))
	if format == "" {
		return app.Signature{State: app.Unsigned}, nil
	}
	sig := app.Signature{State: app.Signed, Format: format}
	// verify-commit fails both for a bad signature and for an unknown key;
	// either way skilus cannot vouch for it.
	if out, err := f.git(ctx, dir, nil, "verify-commit", "--raw", "--end-of-options", commit); err == nil {
		sig.State, sig.VerifiedBy, sig.Signer = app.Verified, "git", signer(string(out))
		return sig, nil
	}
	if s.GitHubAPI != "-" && strings.HasPrefix(src.ID, "github.com/") {
		if ok, err := s.github(ctx, strings.TrimPrefix(src.ID, "github.com/"), commit); err == nil && ok {
			sig.State, sig.VerifiedBy = app.Verified, "github"
		}
	}
	return sig, nil
}

// signatureFormat reads the gpgsig header of a raw commit object.
func signatureFormat(raw string) string {
	header, _, _ := strings.Cut(raw, "\n\n")
	for _, line := range strings.Split(header, "\n") {
		name, value, ok := strings.Cut(line, " ")
		if !ok || (name != "gpgsig" && name != "gpgsig-sha256") {
			continue
		}
		switch {
		case strings.Contains(value, "BEGIN SSH SIGNATURE"):
			return "ssh"
		case strings.Contains(value, "BEGIN SIGNED MESSAGE"):
			return "x509"
		default:
			return "gpg"
		}
	}
	return ""
}

// signer picks the key or identity from git verify-commit --raw output,
// which comes from gpg's status lines or ssh-keygen.
func signer(out string) string {
	for _, line := range strings.Split(out, "\n") {
		if _, rest, ok := strings.Cut(line, "GOODSIG "); ok {
			return strings.TrimSpace(rest)
		}
		if _, rest, ok := strings.Cut(line, `Good "git" signature for `); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

// github asks GitHub whether it verified the commit's signature.
func (s Signatures) github(ctx context.Context, repo, commit string) (bool, error) {
	base := s.GitHubAPI
	if base == "" {
		base = "https://api.github.com"
	}
	owner, name, ok := strings.Cut(repo, "/")
	if !ok || strings.Contains(name, "/") {
		return false, fmt.Errorf("%s is not owner/repo: %w", repo, domain.ErrInvalid)
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/repos/"+owner+"/"+strings.TrimSuffix(name, ".git")+"/commits/"+commit, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "skilus")
	if s.Token != "" {
		req.Header.Set("Authorization", "Bearer "+s.Token)
	}
	client := s.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close() //nolint:errcheck // read-only
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("GitHub API: %s", resp.Status)
	}
	var body struct {
		Commit struct {
			Verification struct {
				Verified bool `json:"verified"`
			} `json:"verification"`
		} `json:"commit"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 5<<20)).Decode(&body); err != nil {
		return false, err
	}
	return body.Commit.Verification.Verified, nil
}
