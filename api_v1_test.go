package servermanager

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/JustaPenguin/assetto-server-manager/internal/testutil"
)

// restoreResultFixtures re-copies fixtures/results/*.json into
// ServerInstallPath/results so the golden tests see pristine input even
// after TestChampionshipManager_* has mutated the working copy in the
// same test binary run.
func restoreResultFixtures(t *testing.T) {
	t.Helper()

	src := filepath.Join("fixtures", "results")
	dst := filepath.Join(ServerInstallPath, "results")

	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dst, err)
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatalf("read %s: %v", src, err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		if err := os.WriteFile(filepath.Join(dst, e.Name()), data, 0o644); err != nil {
			t.Fatalf("write %s: %v", e.Name(), err)
		}
	}
}

// withChiURLParam stuffs a chi URL parameter into the request context so
// chi.URLParam works in tests that bypass the router.
func withChiURLParam(r *http.Request, key, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

// Golden-file coverage for the /api/v1 handlers the Discord bot actually
// consumes (see bot/src/acsm/client.ts). Locks in the JSON shape so a
// future refactor can't silently rename fields or change ordering.
//
// Regenerate with:   go test -run TestAPIv1 -update

// stateServerProcess swaps in a deterministic Event so serverState has
// something non-empty to report. Everything else is delegated to
// dummyServerProcess.
type stateServerProcess struct {
	dummyServerProcess
}

func (stateServerProcess) Event() RaceEvent {
	return &CustomRace{
		RaceConfig: CurrentRaceConfig{
			Track:       "spa",
			TrackLayout: "grand_prix",
			MaxClients:  32,
		},
	}
}

func newAPIv1TestHandler(t *testing.T, sp ServerProcess) *APIv1Handler {
	t.Helper()

	store := NewJSONStore(filepath.Join(t.TempDir(), "store"), filepath.Join(t.TempDir(), "shared"))

	if err := store.UpsertServerOptions(&GlobalServerConfig{Name: "Test Server"}); err != nil {
		t.Fatalf("seed server options: %v", err)
	}

	fixedUpdated := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	races := []*CustomRace{
		{
			Name:    "Spa GP",
			UUID:    uuid.MustParse("11111111-1111-1111-1111-111111111111"),
			Updated: fixedUpdated,
			Starred: true,
			RaceConfig: CurrentRaceConfig{
				Track:       "spa",
				TrackLayout: "grand_prix",
				Cars:        "ferrari_488_gt3;mclaren_650_gt3",
			},
		},
		{
			Name:    "Monza Quick",
			UUID:    uuid.MustParse("22222222-2222-2222-2222-222222222222"),
			Updated: fixedUpdated.Add(-1 * time.Hour),
			RaceConfig: CurrentRaceConfig{
				Track: "monza",
				Cars:  "bmw_z4_gt3",
			},
		},
	}

	for _, r := range races {
		if err := store.UpsertCustomRace(r); err != nil {
			t.Fatalf("seed custom race %s: %v", r.UUID, err)
		}
	}

	rm := NewRaceManager(store, sp, NewCarManager(NewTrackManager(), false, false), NewTrackManager(), &dummyNotificationManager{}, nil)
	return NewAPIv1Handler(store, rm, sp, nil)
}

func doAPIRequest(t *testing.T, h http.HandlerFunc, method, target string) []byte {
	t.Helper()

	req := httptest.NewRequest(method, target, nil)
	rec := httptest.NewRecorder()
	h(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	return rec.Body.Bytes()
}

func TestAPIv1_ServerState(t *testing.T) {
	ServerInstallPath = filepath.Join("cmd", "server-manager", "assetto")

	h := newAPIv1TestHandler(t, stateServerProcess{})
	got := doAPIRequest(t, h.serverState, http.MethodGet, "/api/v1/server/state")

	testutil.CompareGoldenJSON(t, "api_v1_server_state", got)
}

func TestAPIv1_ListCustomRaces(t *testing.T) {
	h := newAPIv1TestHandler(t, dummyServerProcess{})
	got := doAPIRequest(t, h.listCustomRaces, http.MethodGet, "/api/v1/custom-races")

	testutil.CompareGoldenJSON(t, "api_v1_custom_races", got)
}

func TestAPIv1_ListResults(t *testing.T) {
	ServerInstallPath = filepath.Join("cmd", "server-manager", "assetto")
	restoreResultFixtures(t)

	h := newAPIv1TestHandler(t, dummyServerProcess{})
	got := doAPIRequest(t, h.listResults, http.MethodGet, "/api/v1/results?limit=3")

	testutil.CompareGoldenJSON(t, "api_v1_results_list", got)
}

func TestAPIv1_GetResult(t *testing.T) {
	ServerInstallPath = filepath.Join("cmd", "server-manager", "assetto")
	restoreResultFixtures(t)

	h := newAPIv1TestHandler(t, dummyServerProcess{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/results/2019_2_15_21_16_RACE.json", nil)
	// chi.URLParam is read via RouteContext; inject it directly.
	req = withChiURLParam(req, "file", "2019_2_15_21_16_RACE.json")
	rec := httptest.NewRecorder()

	h.getResult(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	testutil.CompareGoldenJSON(t, "api_v1_result_detail", rec.Body.Bytes())
}
