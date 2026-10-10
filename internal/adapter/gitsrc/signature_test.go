package gitsrc_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/colybri/skilus/internal/adapter/gitsrc"
	"github.com/colybri/skilus/internal/app"
)

func TestSignatureUnsigned(t *testing.T) {
	r := newRepo(t)
	r.write("SKILL.md", manifest("demo", "Demo"))
	commit := r.commit("v1")

	sig, err := gitsrc.Signatures{GitHubAPI: "-"}.Signature(context.Background(), r.source(t, ""), commit)
	if err != nil {
		t.Fatal(err)
	}
	if sig.State != app.Unsigned {
		t.Fatalf("sig = %+v", sig)
	}
}

func TestSignatureSSH(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("OpenSSH on Windows checks key file ACLs that temporary directories do not meet")
	}
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("ssh-keygen is not installed")
	}
	r := newRepo(t)
	key := filepath.Join(t.TempDir(), "key")
	if out, err := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-C", "t@example.com", "-f", key).CombinedOutput(); err != nil {
		t.Fatalf("ssh-keygen: %v\n%s", err, out)
	}
	r.write("SKILL.md", manifest("demo", "Demo"))
	r.git("-c", "gpg.format=ssh", "-c", "user.signingkey="+key, "commit", "--quiet", "-S", "-m", "signed")
	commit := r.git("rev-parse", "HEAD")
	ctx := context.Background()
	s := gitsrc.Signatures{GitHubAPI: "-"}

	// Without allowed signers git cannot vouch for the key.
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "empty"))
	sig, err := s.Signature(ctx, r.source(t, ""), commit)
	if err != nil {
		t.Fatal(err)
	}
	if sig.State != app.Signed || sig.Format != "ssh" {
		t.Fatalf("without allowed signers: %+v", sig)
	}

	pub, err := os.ReadFile(key + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	signers := filepath.Join(dir, "allowed")
	if err := os.WriteFile(signers, append([]byte("t@example.com "), pub...), 0o644); err != nil {
		t.Fatal(err)
	}
	global := filepath.Join(dir, "gitconfig")
	if err := os.WriteFile(global, []byte("[gpg \"ssh\"]\n\tallowedSignersFile = "+filepath.ToSlash(signers)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	sig, err = s.Signature(ctx, r.source(t, ""), commit)
	if err != nil {
		t.Fatal(err)
	}
	if sig.State != app.Verified || sig.VerifiedBy != "git" {
		t.Fatalf("with allowed signers: %+v", sig)
	}
}
