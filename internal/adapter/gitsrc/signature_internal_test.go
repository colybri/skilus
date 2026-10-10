package gitsrc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSignatureFormat(t *testing.T) {
	for raw, want := range map[string]string{
		"tree x\nauthor a\n\nmsg gpgsig in body\n":                                "",
		"tree x\ngpgsig -----BEGIN PGP SIGNATURE-----\n \n -----END PGP\n\nmsg":   "gpg",
		"tree x\ngpgsig -----BEGIN SSH SIGNATURE-----\n\nmsg":                     "ssh",
		"tree x\ngpgsig-sha256 -----BEGIN SIGNED MESSAGE-----\n\nmsg":             "x509",
		"tree x\nmergetag object y\n gpgsig -----BEGIN PGP SIGNATURE-----\n\nmsg": "",
	} {
		if got := signatureFormat(raw); got != want {
			t.Errorf("signatureFormat(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestGitHubVerification(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/repos/o/r/commits/aaa":
			_, _ = w.Write([]byte(`{"commit":{"verification":{"verified":true,"reason":"valid"}}}`))
		case "/repos/o/r/commits/bbb":
			_, _ = w.Write([]byte(`{"commit":{"verification":{"verified":false,"reason":"unknown_key"}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	s := Signatures{GitHubAPI: srv.URL, Token: "tok", Client: srv.Client()}
	if ok, err := s.github(context.Background(), "o/r.git", "aaa"); !ok || err != nil {
		t.Errorf("aaa = %v, %v", ok, err)
	}
	if ok, err := s.github(context.Background(), "o/r", "bbb"); ok || err != nil {
		t.Errorf("bbb = %v, %v", ok, err)
	}
	if _, err := s.github(context.Background(), "o/r", "ccc"); err == nil {
		t.Error("404 should be an error")
	}
}
