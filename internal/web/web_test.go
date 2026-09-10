package web_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/atvirokodosprendimai/app-taskerv1/internal/auth"
	"github.com/atvirokodosprendimai/app-taskerv1/internal/store/storetest"
	"github.com/atvirokodosprendimai/app-taskerv1/internal/tracking"
	"github.com/atvirokodosprendimai/app-taskerv1/internal/web"
	"github.com/atvirokodosprendimai/app-taskerv1/internal/web/view"
)

// password is every test account's password.
const password = "correct horse battery"

// harness runs the application behind a real HTTP server — the real routes,
// middleware, sessions and migrations — so a test reaches what a browser would.
type harness struct {
	url    string
	users  *auth.Repo
	timers *tracking.Repo
	// shutdown does what the server does as it shuts down: it ends every open
	// stream.
	shutdown func()
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	db := storetest.Open(t)
	users := auth.NewRepo(db.Read, db.Write)
	timers := tracking.NewRepo(db.Read, db.Write)
	closing := make(chan struct{})
	var once sync.Once
	app := &web.App{
		Log:      slog.New(slog.DiscardHandler),
		Sessions: web.NewSessions(db.Write, false),
		Users:    users,
		Tracking: timers,
		Auth:     auth.NewService(users),
		Tracker:  tracking.NewService(timers),
		Bus:      web.NewBus(),
		Now:      time.Now,
		Closing:  closing,
	}
	srv := httptest.NewServer(app.Routes())
	h := &harness{
		url:      srv.URL,
		users:    users,
		timers:   timers,
		shutdown: func() { once.Do(func() { close(closing) }) },
	}
	// Cleanups run in reverse order: streams end, then the server stops waiting
	// for its requests, then the database closes.
	t.Cleanup(srv.Close)
	t.Cleanup(h.shutdown)
	return h
}

// response is what came back from one request.
type response struct {
	status int
	header http.Header
	body   string
}

// browser returns a client with its own cookie jar, which leaves redirects for
// the test to see instead of following them.
func (h *harness) browser(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	return &http.Client{
		Jar:           jar,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func (h *harness) send(t *testing.T, c *http.Client, req *http.Request) response {
	t.Helper()
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", req.Method, req.URL.Path, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s %s: %v", req.Method, req.URL.Path, err)
	}
	return response{status: resp.StatusCode, header: resp.Header, body: string(b)}
}

// page loads a page the way following a link does.
func (h *harness) page(t *testing.T, c *http.Client, path string) response {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, h.url+path, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	return h.send(t, c, req)
}

// action sends a request the way a datastar action does: with the
// Datastar-Request header, and the signals as JSON — in the body, or in the
// datastar query parameter on a GET.
func (h *harness) action(t *testing.T, c *http.Client, method, path string, signals any) response {
	t.Helper()
	payload, err := json.Marshal(signals)
	if err != nil {
		t.Fatalf("marshal signals: %v", err)
	}
	target := h.url + path
	var body io.Reader
	if method == http.MethodGet {
		sep := "?"
		if strings.Contains(path, "?") {
			sep = "&"
		}
		target += sep + "datastar=" + url.QueryEscape(string(payload))
	} else {
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(t.Context(), method, target, body)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Datastar-Request", "true")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return h.send(t, c, req)
}

// navigatesTo reports whether a response tells the browser to go to path.
func navigatesTo(body, path string) bool {
	return strings.Contains(body, fmt.Sprintf("window.location.href = %q", path))
}

// register creates an account through the registration action, and returns the
// signed-in browser and the account.
func (h *harness) register(t *testing.T, email string) (*http.Client, auth.User) {
	t.Helper()
	c := h.browser(t)
	r := h.action(t, c, http.MethodPost, "/register", map[string]string{
		"registerEmail":    email,
		"registerPassword": password,
		"registerTimezone": "Europe/Vilnius",
	})
	if r.status != http.StatusOK || !navigatesTo(r.body, "/") {
		t.Fatalf("register %s = %d, want a navigation to /:\n%s", email, r.status, r.body)
	}
	u, err := h.users.ByEmail(t.Context(), email)
	if err != nil {
		t.Fatalf("load %s: %v", email, err)
	}
	return c, u
}

// addCompany adds a company through the page's action and returns it as stored.
func (h *harness) addCompany(t *testing.T, c *http.Client, u auth.User, name string) tracking.Company {
	t.Helper()
	r := h.action(t, c, http.MethodPost, "/companies", map[string]string{"newCompany": name})
	if r.status != http.StatusOK {
		t.Fatalf("add company %q = %d", name, r.status)
	}
	companies, err := h.timers.Companies(t.Context(), u.ID)
	if err != nil {
		t.Fatalf("list companies: %v", err)
	}
	for _, co := range companies {
		if co.Name == name {
			return co
		}
	}
	t.Fatalf("company %q was not created:\n%s", name, r.body)
	return tracking.Company{}
}

// start presses Start on a company with task typed into its box.
func (h *harness) start(t *testing.T, c *http.Client, companyID int64, task string) response {
	t.Helper()
	return h.action(t, c, http.MethodPost, fmt.Sprintf("/companies/%d/timers", companyID),
		map[string]string{view.TaskSignal(companyID): task})
}

func (h *harness) running(t *testing.T, u auth.User) []tracking.Entry {
	t.Helper()
	entries, err := h.timers.Running(t.Context(), u.ID)
	if err != nil {
		t.Fatalf("list running timers: %v", err)
	}
	return entries
}

func TestRegisteringSignsInAndOpensTheTimers(t *testing.T) {
	h := newHarness(t)
	c, u := h.register(t, "ada@example.com")

	if u.Timezone != "Europe/Vilnius" {
		t.Errorf("timezone = %q, want the zone the browser reported", u.Timezone)
	}
	r := h.page(t, c, "/")
	if r.status != http.StatusOK {
		t.Fatalf("GET / signed in = %d", r.status)
	}
	for _, want := range []string{"ada@example.com", `id="running-timers"`, `id="company-list"`, `id="flash"`} {
		if !strings.Contains(r.body, want) {
			t.Errorf("the timers page is missing %q", want)
		}
	}
}

func TestSignedOutVisitorsAreSentToSignIn(t *testing.T) {
	h := newHarness(t)
	c := h.browser(t)

	if r := h.page(t, c, "/"); r.status != http.StatusSeeOther || r.header.Get("Location") != "/login" {
		t.Errorf("GET / signed out = %d to %q, want 303 to /login", r.status, r.header.Get("Location"))
	}
	// A datastar action would swallow a 303, so it is told to navigate instead.
	r := h.action(t, c, http.MethodPost, "/companies", map[string]string{"newCompany": "Acme"})
	if r.status != http.StatusOK || !navigatesTo(r.body, "/login") {
		t.Errorf("POST /companies signed out = %d, want a navigation to /login:\n%s", r.status, r.body)
	}
	if r := h.page(t, c, "/login"); r.status != http.StatusOK || !strings.Contains(r.body, `data-bind="loginEmail"`) {
		t.Errorf("GET /login = %d, want the sign-in form", r.status)
	}
}

func TestAWriteWithoutTheDatastarHeaderIsRefused(t *testing.T) {
	h := newHarness(t)
	c, u := h.register(t, "ada@example.com")

	// The same browser and route WITH the header succeeds, so the refusal below
	// is the header's doing rather than a broken route or session.
	h.addCompany(t, c, u, "Acme")

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, h.url+"/companies",
		strings.NewReader(`{"newCompany":"Forged"}`))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if r := h.send(t, c, req); r.status != http.StatusForbidden {
		t.Errorf("a POST without Datastar-Request = %d, want 403", r.status)
	}
	companies, err := h.timers.Companies(t.Context(), u.ID)
	if err != nil {
		t.Fatalf("list companies: %v", err)
	}
	if len(companies) != 1 {
		t.Errorf("companies = %+v, want only Acme", companies)
	}
}

func TestEverySignInFailureReadsTheSame(t *testing.T) {
	h := newHarness(t)
	h.register(t, "ada@example.com")
	c := h.browser(t)
	login := func(email, pw string) string {
		t.Helper()
		return h.action(t, c, http.MethodPost, "/login", map[string]string{"loginEmail": email, "loginPassword": pw}).body
	}

	wrongPassword := login("ada@example.com", "not the password")
	noAccount := login("nobody@example.com", password)
	malformed := login("not an address", password)

	if !strings.Contains(wrongPassword, "That email and password do not match an account.") {
		t.Fatalf("a wrong password did not get the sign-in refusal:\n%s", wrongPassword)
	}
	if noAccount != wrongPassword || malformed != wrongPassword {
		t.Errorf("sign-in failures differ, which tells a stranger who has an account:\nwrong password: %s\nno account: %s\nmalformed: %s",
			wrongPassword, noAccount, malformed)
	}
	if body := login("ADA@example.com", password); !navigatesTo(body, "/") {
		t.Errorf("the right password in another letter case did not sign in:\n%s", body)
	}
}

func TestSigningOutRevokesTheSessionEverywhere(t *testing.T) {
	h := newHarness(t)
	c, _ := h.register(t, "ada@example.com")
	base, err := url.Parse(h.url)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}
	// A copy of the cookie, as a stolen one or another device would hold it.
	stolen := h.browser(t)
	stolen.Jar.SetCookies(base, c.Jar.Cookies(base))
	if r := h.page(t, stolen, "/"); r.status != http.StatusOK {
		t.Fatalf("the copied cookie did not sign in before sign-out (%d), so the check below would prove nothing", r.status)
	}

	if r := h.action(t, c, http.MethodPost, "/logout", map[string]string{}); !navigatesTo(r.body, "/login") {
		t.Fatalf("sign out did not navigate to /login:\n%s", r.body)
	}
	if r := h.page(t, stolen, "/"); r.status != http.StatusSeeOther {
		t.Errorf("the copied cookie after sign-out = %d, want 303 to sign-in", r.status)
	}
}

func TestSeveralTimersRunAtOnceAndOnlyTheirOwnerCanStopThem(t *testing.T) {
	h := newHarness(t)
	ada, adaUser := h.register(t, "ada@example.com")
	bob, bobUser := h.register(t, "bob@example.com")
	acme := h.addCompany(t, ada, adaUser, "Acme")
	beta := h.addCompany(t, ada, adaUser, "Beta")

	r := h.start(t, ada, acme.ID, "Invoices")
	if r.status != http.StatusOK {
		t.Fatalf("start = %d", r.status)
	}
	// The box the task was typed into is cleared by the server once it is saved.
	if cleared := fmt.Sprintf("%q:\"\"", view.TaskSignal(acme.ID)); !strings.Contains(r.body, cleared) {
		t.Errorf("start did not clear %s:\n%s", view.TaskSignal(acme.ID), r.body)
	}
	h.start(t, ada, beta.ID, "")
	h.start(t, ada, acme.ID, "Code review")

	if got := h.running(t, adaUser); len(got) != 3 {
		t.Fatalf("running = %d timers, want 3 at once", len(got))
	}
	page := h.page(t, ada, "/").body
	if n := strings.Count(page, `class="timer"`); n != 3 {
		t.Errorf("the page lists %d running timers, want 3", n)
	}
	for _, want := range []string{"Invoices", "Code review", "No description"} {
		if !strings.Contains(page, want) {
			t.Errorf("the page does not show %q", want)
		}
	}

	// Bob can neither start a timer on Ada's company nor stop Ada's timer.
	if r := h.start(t, bob, acme.ID, "not mine"); !strings.Contains(r.body, "no longer exists") {
		t.Errorf("starting on someone else's company was not refused:\n%s", r.body)
	}
	if got := h.running(t, bobUser); len(got) != 0 {
		t.Errorf("Bob has %d running timers after starting on Ada's company, want 0", len(got))
	}
	target := h.running(t, adaUser)[0]
	stop := fmt.Sprintf("/timers/%d/stop", target.ID)
	if r := h.action(t, bob, http.MethodPost, stop, map[string]string{}); !strings.Contains(r.body, "already been stopped") {
		t.Errorf("stopping someone else's timer was not refused:\n%s", r.body)
	}
	if got := h.running(t, adaUser); len(got) != 3 {
		t.Fatalf("Ada has %d running timers after Bob's stop, want all 3", len(got))
	}

	// Ada stops one and the other two keep running.
	h.action(t, ada, http.MethodPost, stop, map[string]string{})
	if got := h.running(t, adaUser); len(got) != 2 {
		t.Errorf("running after one stop = %d, want 2", len(got))
	}
	if r := h.action(t, ada, http.MethodPost, stop, map[string]string{}); !strings.Contains(r.body, "already been stopped") {
		t.Errorf("stopping a stopped timer was not answered on the page:\n%s", r.body)
	}
}

// stream is an open dashboard stream, parsed into events as they arrive.
type stream struct {
	events <-chan sseEvent
}

type sseEvent struct {
	name string
	data string
}

func (h *harness) openStream(t *testing.T, c *http.Client) *stream {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, h.url+"/stream", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Datastar-Request", "true")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("GET /stream = %d %q, want 200 text/event-stream", resp.StatusCode, resp.Header.Get("Content-Type"))
	}

	events := make(chan sseEvent, 256)
	go func() {
		defer close(events)
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
		var ev sseEvent
		for sc.Scan() {
			switch line := sc.Text(); {
			case line == "":
				if ev.name != "" {
					events <- ev
				}
				ev = sseEvent{}
			case strings.HasPrefix(line, "event: "):
				ev.name = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				ev.data += strings.TrimPrefix(line, "data: ") + "\n"
			}
		}
	}()
	return &stream{events: events}
}

// next returns the next event whose data contains want, skipping the others.
func (s *stream) next(t *testing.T, want string, within time.Duration) sseEvent {
	t.Helper()
	deadline := time.After(within)
	for {
		select {
		case ev, ok := <-s.events:
			if !ok {
				t.Fatalf("the stream ended while waiting for %q", want)
			}
			if strings.Contains(ev.data, want) {
				return ev
			}
		case <-deadline:
			t.Fatalf("no event containing %q within %v", want, within)
		}
	}
}

// quiet fails if an event containing unwanted arrives within d.
func (s *stream) quiet(t *testing.T, unwanted string, d time.Duration) {
	t.Helper()
	deadline := time.After(d)
	for {
		select {
		case ev, ok := <-s.events:
			if !ok {
				t.Fatalf("the stream ended while checking it stayed quiet")
			}
			if strings.Contains(ev.data, unwanted) {
				t.Fatalf("unexpected event containing %q:\n%s", unwanted, ev.data)
			}
		case <-deadline:
			return
		}
	}
}

func TestTheStreamShowsAChangeMadeInAnotherTab(t *testing.T) {
	h := newHarness(t)
	ada, adaUser := h.register(t, "ada@example.com")
	bob, _ := h.register(t, "bob@example.com")

	adaStream := h.openStream(t, ada)
	bobStream := h.openStream(t, bob)
	adaStream.next(t, `id="company-list"`, 5*time.Second)
	bobStream.next(t, `id="company-list"`, 5*time.Second)

	h.addCompany(t, ada, adaUser, "Acme")

	adaStream.next(t, "Acme", 5*time.Second)
	// Ada's stream receiving it is what makes Bob's silence mean tenancy rather
	// than a stream that delivers nothing.
	bobStream.quiet(t, "Acme", 1500*time.Millisecond)
}

func TestTheStreamTicksEverySecondWhileATimerRuns(t *testing.T) {
	h := newHarness(t)
	ada, adaUser := h.register(t, "ada@example.com")
	acme := h.addCompany(t, ada, adaUser, "Acme")
	h.start(t, ada, acme.ID, "Invoices")

	s := h.openStream(t, ada)
	s.next(t, `id="running-timers"`, 5*time.Second)
	first := s.next(t, `id="running-timers"`, 2*time.Second)
	second := s.next(t, `id="running-timers"`, 2*time.Second)
	if first.data == second.data {
		t.Errorf("two ticks a second apart rendered the same timers:\n%s", second.data)
	}
	if !strings.Contains(second.data, "Invoices") {
		t.Errorf("a tick does not show the running timer:\n%s", second.data)
	}
}

func TestShuttingDownEndsOpenStreams(t *testing.T) {
	h := newHarness(t)
	ada, _ := h.register(t, "ada@example.com")
	s := h.openStream(t, ada)
	s.next(t, `id="company-list"`, 5*time.Second)

	h.shutdown()

	deadline := time.After(2 * time.Second)
	for {
		select {
		case _, ok := <-s.events:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("the stream was still open two seconds after shutdown began; it would hold the server's shutdown open")
		}
	}
}

func TestHistoryFollowsTheFilter(t *testing.T) {
	h := newHarness(t)
	ada, adaUser := h.register(t, "ada@example.com")
	acme := h.addCompany(t, ada, adaUser, "Acme")
	h.start(t, ada, acme.ID, "Invoices")
	now := time.Now().In(adaUser.Location())

	if r := h.page(t, ada, "/history?mode=day&day="+now.Format(tracking.DateLayout)); r.status != http.StatusOK ||
		!strings.Contains(r.body, "Invoices") {
		t.Fatalf("today's history = %d, want it to list the running timer:\n%s", r.status, r.body)
	}

	results := func(path string, s view.HistorySignals) string {
		t.Helper()
		r := h.action(t, ada, http.MethodGet, path, s)
		if r.status != http.StatusOK {
			t.Fatalf("GET %s = %d", path, r.status)
		}
		return r.body
	}
	expect := func(body string, wants ...string) {
		t.Helper()
		for _, want := range wants {
			if !strings.Contains(body, want) {
				t.Errorf("results are missing %q:\n%s", want, body)
			}
		}
	}

	// A new filter re-renders the results and keeps the address bar in step.
	expect(results("/history/results", view.HistorySignals{Mode: "year", Year: "1999"}),
		"No time recorded for 1999", "replaceState", "/history?mode=year&year=1999")

	// Next moves a month on, and the inputs move with the results.
	expect(results("/history/results?shift=1", view.HistorySignals{Mode: "month", Month: "2026-09"}),
		`"histMonth":"2026-10"`, "October 2026")

	// A preset changes the period and keeps the company.
	company := strconv.FormatInt(acme.ID, 10)
	expect(results("/history/results?preset=year", view.HistorySignals{Mode: "day", Day: "2020-01-01", Company: company}),
		`"histMode":"year"`, `"histYear":"`+now.Format("2006")+`"`, `"histCompany":"`+company+`"`, "Invoices")

	// A range that ends before it starts is answered on the page, and — asking
	// for no move — leaves the inputs as they are.
	body := results("/history/results", view.HistorySignals{Mode: "range", From: "2026-09-10", To: "2026-09-01"})
	expect(body, "That is not a period we can show")
	if strings.Contains(body, "datastar-patch-signals") {
		t.Errorf("a request that asked for no move patched the filter's inputs:\n%s", body)
	}
}

func TestHistoryShowsOnlyTheUsersOwnTime(t *testing.T) {
	h := newHarness(t)
	ada, adaUser := h.register(t, "ada@example.com")
	bob, _ := h.register(t, "bob@example.com")
	acme := h.addCompany(t, ada, adaUser, "Acme")
	h.start(t, ada, acme.ID, "Invoices")

	filter := view.HistorySignals{
		Mode:    "year",
		Year:    time.Now().In(adaUser.Location()).Format("2006"),
		Company: strconv.FormatInt(acme.ID, 10),
	}
	if r := h.action(t, ada, http.MethodGet, "/history/results", filter); !strings.Contains(r.body, "Invoices") {
		t.Fatalf("Ada's own history does not show her entry, so Bob's empty answer below would prove nothing:\n%s", r.body)
	}
	r := h.action(t, bob, http.MethodGet, "/history/results", filter)
	if strings.Contains(r.body, "Invoices") || !strings.Contains(r.body, "No time recorded") {
		t.Errorf("Bob's history, naming Ada's company, = \n%s\nwant nothing recorded", r.body)
	}
}
