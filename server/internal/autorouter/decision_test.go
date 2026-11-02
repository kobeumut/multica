package autorouter

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// Opt-in: requires the real local model; never turn a skipped smoke into a quality claim.
func TestLocalDecisionSmoke(t *testing.T) {
	if os.Getenv("MULTICA_AUTOROUTER_SMOKE") != "1" {
		t.Skip("set MULTICA_AUTOROUTER_SMOKE=1 after pulling " + Model)
	}
	// Match handler formatting and the default category descriptions, not bare labels.
	candidates := []Candidate{
		{ID: "entry", Name: "Entry", Description: "Simple tasks: fix typos, edit documentation, make small code changes and give short explanations.", Categories: []string{"Entry-level: Simple tasks: fix typos, edit documentation, make small code changes and give short explanations."}},
		{ID: "normal", Name: "General", Description: "Standard software implementation, tests and maintenance.", Categories: []string{"Normal: Standard implementation and general tasks."}},
		{ID: "advanced", Name: "Architect", Description: "Complex distributed systems architecture, security and difficult debugging.", Categories: []string{"Advanced: Complex architecture, debugging and high-risk changes."}},
		{ID: "mobile", Name: "Mobile developer", Description: "React Native, Android, iOS and mobile accessibility.", Categories: []string{"Mobile Developer: React Native and Android/iOS application development."}},
		{ID: "pm", Name: "Project manager", Description: "Sprint planning, backlog prioritization and project coordination.", Categories: []string{"Project Manager: Sprint planning and backlog prioritization."}},
	}
	// Production choices are UUIDs, not semantic labels that can hint at the answer.
	ids := make(map[string]string, len(candidates))
	for i := range candidates {
		name := candidates[i].ID
		candidates[i].ID = fmt.Sprintf("10000000-0000-4000-8000-%012d", i+1)
		ids[name] = candidates[i].ID
	}
	for _, tc := range []struct{ state, want string }{
		{"Fix a spelling mistake in this README sentence.", "entry"},
		{"Implement a standard REST endpoint with input validation and unit tests.", "normal"},
		{"Design a multi-region distributed transaction protocol and review its failure modes.", "advanced"},
		{"React Native Android ekranında erişilebilirlik etiketlerini düzelt.", "mobile"},
		{"Sprint planını ve backlog önceliklerini hazırla.", "pm"},
		{"Give me a recipe for chocolate cake.", ""},
	} {
		name := tc.want
		if name == "" {
			name = "no-match"
		}
		t.Run(name, func(t *testing.T) {
			start := time.Now()
			got, err := Decide(context.Background(), Input{State: tc.state, Candidates: candidates})
			if tc.want == "" {
				if got != "" || err == nil || err.Error() != "no suitable agent; choose an agent explicitly" {
					t.Fatalf("expected no-match, got %q (%v)", got, err)
				}
				t.Logf("correctly declined in %s", time.Since(start))
				return
			}
			if err != nil || got != ids[tc.want] {
				t.Fatalf("got %q (%v), want %s (%s); duration %s", got, err, tc.want, ids[tc.want], time.Since(start))
			}
			t.Logf("selected %s (%s) in %s", tc.want, got, time.Since(start))
		})
	}
}

func TestDecision(t *testing.T) {
	input := Input{State: "React Native uygulamasında hata var", Candidates: []Candidate{{ID: "mobile", Name: "Mobile", Description: "React Native developer", Categories: []string{"Mobile developer", "Advanced"}}}}
	for _, tc := range []struct {
		name, response string
		status         int
		want           string
	}{
		{"choice", `{"answers":{"route":{"choice":"agent_1","confidence":0.8}}}`, 200, "mobile"},
		{"no match", `{"answers":{"route":{"choice":"none","confidence":0.9}}}`, 200, ""},
		{"invented agent", `{"answers":{"route":{"choice":"private","confidence":0.9}}}`, 200, ""},
		{"invented option", `{"answers":{"route":{"choice":"agent_2","confidence":0.9}}}`, 200, ""},
		{"raw agent ID", `{"answers":{"route":{"choice":"mobile","confidence":0.9}}}`, 200, ""},
		{"missing confidence", `{"answers":{"route":{"choice":"agent_1"}}}`, 200, ""},
		{"invalid confidence", `{"answers":{"route":{"choice":"agent_1","confidence":2}}}`, 200, ""},
		{"wrong shape", `{"message":{"content":"mobile"}}`, 200, ""},
		{"malformed", "{", 200, ""}, {"missing model", "", 404, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.URL.Path != "/v1/systemone" {
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
				}
				var body struct {
					Model     string `json:"model"`
					State     string `json:"state"`
					Questions map[string]struct {
						Type     string            `json:"type"`
						Criteria map[string]string `json:"criteria"`
					} `json:"questions"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if body.Model != "d1-omni:600m" || body.State != input.State || body.Questions["route"].Type != "choice" || len(body.Questions["route"].Criteria) != 2 {
					t.Errorf("wrong request: %+v", body)
				}
				if got := body.Questions["route"].Criteria["agent_1"]; got != "Mobile: React Native developer. Categories: Mobile developer, Advanced" {
					t.Errorf("wrong candidate description: %q", got)
				}
				if body.Questions["route"].Criteria["none"] == "" {
					t.Error("missing no-match option")
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.response))
			}))
			defer server.Close()
			got, err := decide(context.Background(), server.Client(), server.URL+"/v1/systemone", input)
			if got != tc.want || (err == nil) != (tc.want != "") {
				t.Fatalf("got %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestDecisionRejectsInvalidInputAndCancellation(t *testing.T) {
	for _, input := range []Input{{}, {State: "x"}, {State: strings.Repeat("x", MaxStateBytes+1), Candidates: []Candidate{{ID: "a"}}}, {State: "x", Candidates: []Candidate{{ID: "a"}, {ID: "a"}}}} {
		if input.Validate() == nil {
			t.Fatalf("accepted invalid input: %+v", input)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	_, err := Decide(ctx, Input{State: "x", Candidates: []Candidate{{ID: "a"}}})
	if err == nil {
		t.Fatal("cancelled request succeeded")
	}
}
