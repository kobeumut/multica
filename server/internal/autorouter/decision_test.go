package autorouter

import (
	"context"
	"encoding/json"
	"errors"
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
				if got != "" || !errors.Is(err, ErrNoMatch) {
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
	input := Input{State: "React Native uygulamasında hata var", Candidates: []Candidate{
		{ID: "mobile", Name: "Mobile", Description: "React Native developer", Categories: []string{"Mobile developer", "Advanced"}},
		{ID: "pm", Name: "PM", Description: "Planning"},
	}}
	const related = `"related":{"type":"noul","noul":0.9}`
	for _, tc := range []struct {
		name, response string
		status         int
		want           string
		wantNoMatch    bool
	}{
		{"choice", `{"answers":{` + related + `,"route":{"choice":"agent_1","confidence":0.8}}}`, 200, "mobile", false},
		{"second choice", `{"answers":{` + related + `,"route":{"choice":"agent_2","confidence":0.8}}}`, 200, "pm", false},
		{"unrelated request", `{"answers":{"related":{"type":"noul","noul":0.1},"route":{"choice":"agent_1","confidence":0.8}}}`, 200, "", true},
		{"low relevance is still routed above the floor", `{"answers":{"related":{"type":"noul","noul":0.3},"route":{"choice":"agent_1","confidence":0.8}}}`, 200, "mobile", false},
		{"invented agent", `{"answers":{` + related + `,"route":{"choice":"private","confidence":0.9}}}`, 200, "", false},
		{"invented option", `{"answers":{` + related + `,"route":{"choice":"agent_3","confidence":0.9}}}`, 200, "", false},
		{"raw agent ID", `{"answers":{` + related + `,"route":{"choice":"mobile","confidence":0.9}}}`, 200, "", false},
		{"none is not an option", `{"answers":{` + related + `,"route":{"choice":"none","confidence":0.9}}}`, 200, "", false},
		{"missing confidence", `{"answers":{` + related + `,"route":{"choice":"agent_1"}}}`, 200, "", false},
		{"invalid confidence", `{"answers":{` + related + `,"route":{"choice":"agent_1","confidence":2}}}`, 200, "", false},
		{"missing route answer", `{"answers":{` + related + `}}`, 200, "", false},
		{"missing relevance answer", `{"answers":{"route":{"choice":"agent_1","confidence":0.8}}}`, 200, "", false},
		{"missing relevance probability", `{"answers":{"related":{"type":"noul"},"route":{"choice":"agent_1","confidence":0.8}}}`, 200, "", false},
		{"relevance probability above one", `{"answers":{"related":{"type":"noul","noul":1.2},"route":{"choice":"agent_1","confidence":0.8}}}`, 200, "", false},
		{"negative relevance probability", `{"answers":{"related":{"type":"noul","noul":-0.1},"route":{"choice":"agent_1","confidence":0.8}}}`, 200, "", false},
		{"wrong shape", `{"message":{"content":"mobile"}}`, 200, "", false},
		{"malformed", "{", 200, "", false}, {"missing model", "", 404, "", false},
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
						Type         string            `json:"type"`
						Instructions string            `json:"instructions"`
						Criteria     map[string]string `json:"criteria"`
					} `json:"questions"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				route, relevance := body.Questions["route"], body.Questions["related"]
				if body.Model != "d1-omni:600m" || body.State != input.State || len(body.Questions) != 2 || route.Type != "choice" || relevance.Type != "noul" {
					t.Errorf("wrong request: %+v", body)
				}
				if len(route.Criteria) != 2 {
					t.Errorf("route options = %v, want only the two agents", route.Criteria)
				}
				if _, ok := route.Criteria["none"]; ok {
					t.Error("route offers a none option; no-match belongs to the relevance question")
				}
				if got := route.Criteria["agent_1"]; got != "Mobile: React Native developer. Categories: Mobile developer, Advanced" {
					t.Errorf("wrong candidate description: %q", got)
				}
				if relevance.Criteria["true"] == "" || relevance.Criteria["false"] == "" {
					t.Errorf("relevance question needs true and false descriptions: %v", relevance.Criteria)
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.response))
			}))
			defer server.Close()
			got, err := decide(context.Background(), server.Client(), server.URL+"/v1/systemone", input)
			if got != tc.want || (err == nil) != (tc.want != "") || errors.Is(err, ErrNoMatch) != tc.wantNoMatch {
				t.Fatalf("got %q, %v; want %q (no match %v)", got, err, tc.want, tc.wantNoMatch)
			}
		})
	}
}

// D1 rejects a choice question with one option, so a lone candidate is only
// asked whether the request is routable work at all.
func TestDecisionSingleCandidateSkipsChoice(t *testing.T) {
	input := Input{State: "Fix the typo in README", Candidates: []Candidate{{ID: "only", Name: "Only"}}}
	for _, tc := range []struct {
		name, response string
		want           string
		wantNoMatch    bool
	}{
		{"routable", `{"answers":{"related":{"type":"noul","noul":0.9}}}`, "only", false},
		{"unrelated", `{"answers":{"related":{"type":"noul","noul":0.05}}}`, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					Questions map[string]json.RawMessage `json:"questions"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if _, ok := body.Questions["route"]; ok || len(body.Questions) != 1 {
					t.Errorf("a lone candidate must send only the relevance question, got %v", body.Questions)
				}
				_, _ = w.Write([]byte(tc.response))
			}))
			defer server.Close()
			got, err := decide(context.Background(), server.Client(), server.URL, input)
			if got != tc.want || errors.Is(err, ErrNoMatch) != tc.wantNoMatch || (err != nil) != tc.wantNoMatch {
				t.Fatalf("got %q, %v; want %q (no match %v)", got, err, tc.want, tc.wantNoMatch)
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

// Result crosses the daemon/server boundary as JSON. Both halves are deployed
// together, but an older daemon never sends no_match and must still decode.
func TestResultWireFormat(t *testing.T) {
	for _, tc := range []struct {
		name   string
		result Result
		want   string
	}{
		{"no match", Result{Error: ErrNoMatch.Error(), NoMatch: true}, `{"error":"no suitable agent; choose an agent explicitly","no_match":true}`},
		{"failure", Result{Error: "unavailable"}, `{"error":"unavailable"}`},
		{"selection", Result{AgentID: "a"}, `{"agent_id":"a"}`},
	} {
		got, err := json.Marshal(tc.result)
		if err != nil || string(got) != tc.want {
			t.Errorf("%s: %s (%v), want %s", tc.name, got, err, tc.want)
		}
	}
	var older Result
	if err := json.Unmarshal([]byte(`{"error":"no suitable agent; choose an agent explicitly"}`), &older); err != nil || older.NoMatch {
		t.Errorf("a result without no_match decoded as %+v (%v)", older, err)
	}
}
