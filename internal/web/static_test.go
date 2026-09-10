package web_test

import (
	"encoding/json"
	"image"
	_ "image/png"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

func TestTheWebAppManifestAndItsIconsAreServedToAnyone(t *testing.T) {
	h := newHarness(t)
	// Signed out: a browser fetches a manifest and its icons without the session.
	c := h.browser(t)

	login := h.page(t, c, "/login")
	for _, want := range []string{`rel="manifest" href="/static/manifest.webmanifest?v=9346192d"`, `name="theme-color" content="#6366f1"`} {
		if !strings.Contains(login.body, want) {
			t.Errorf("the sign-in page's head is missing %s", want)
		}
	}

	r := h.page(t, c, "/static/manifest.webmanifest?v=9346192d")
	if r.status != http.StatusOK || r.header.Get("Content-Type") != "application/manifest+json" {
		t.Fatalf("GET the manifest = %d %q, want 200 application/manifest+json", r.status, r.header.Get("Content-Type"))
	}
	var m struct {
		Name     string `json:"name"`
		StartURL string `json:"start_url"`
		Icons    []struct {
			Src   string `json:"src"`
			Sizes string `json:"sizes"`
			Type  string `json:"type"`
		} `json:"icons"`
	}
	if err := json.Unmarshal([]byte(r.body), &m); err != nil {
		t.Fatalf("the manifest is not JSON: %v", err)
	}
	if m.Name != "Tasker" || m.StartURL != "/" || len(m.Icons) != 4 {
		t.Fatalf("manifest = %+v, want Tasker starting at / with four icons", m)
	}
	for _, icon := range m.Icons {
		r := h.page(t, c, icon.Src)
		if r.status != http.StatusOK || r.header.Get("Content-Type") != icon.Type {
			t.Errorf("GET %s = %d %q, want 200 %s", icon.Src, r.status, r.header.Get("Content-Type"), icon.Type)
			continue
		}
		if icon.Sizes == "any" {
			continue
		}
		// A launcher trusts the declared size; an icon rendered at another one is
		// scaled, and blurred, without a word.
		cfg, _, err := image.DecodeConfig(strings.NewReader(r.body))
		if err != nil {
			t.Errorf("%s does not decode as an image: %v", icon.Src, err)
			continue
		}
		if got := strconv.Itoa(cfg.Width) + "x" + strconv.Itoa(cfg.Height); got != icon.Sizes {
			t.Errorf("%s is %s, but the manifest says %s", icon.Src, got, icon.Sizes)
		}
	}
}
