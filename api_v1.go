package servermanager

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/JustaPenguin/assetto-server-manager/pkg/udp"
	"github.com/go-chi/chi/v5"
	"github.com/sirupsen/logrus"
)

// APIv1Handler exposes a stable JSON API for external integrations such as the
// Discord bot. All routes are stateless and authenticated by
// BearerTokenMiddleware — they do not rely on the session-cookie auth used by
// the web UI.
type APIv1Handler struct {
	store         Store
	raceManager   *RaceManager
	serverProcess ServerProcess
	raceControl   *RaceControl
}

func NewAPIv1Handler(store Store, raceManager *RaceManager, serverProcess ServerProcess, raceControl *RaceControl) *APIv1Handler {
	return &APIv1Handler{
		store:         store,
		raceManager:   raceManager,
		serverProcess: serverProcess,
		raceControl:   raceControl,
	}
}

func writeAPIJSON(w http.ResponseWriter, status int, body interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		logrus.WithError(err).Error("api/v1: encode response failed")
	}
}

func writeAPIError(w http.ResponseWriter, status int, msg string) {
	writeAPIJSON(w, status, map[string]string{"error": msg})
}

// ---------- /api/v1/server/state ----------

type apiServerState struct {
	OK          bool      `json:"ok"`
	IsRunning   bool      `json:"is_running"`
	ServerName  string    `json:"server_name"`
	Track       string    `json:"track"`
	TrackLayout string    `json:"track_layout"`
	SessionType string    `json:"session_type"`
	NumDrivers  int       `json:"num_drivers"`
	MaxClients  int       `json:"max_clients"`
	Uptime      string    `json:"uptime"`
	Version     string    `json:"version"`
	GoVersion   string    `json:"go_version"`
	OS          string    `json:"os"`
	Now         time.Time `json:"now"`
}

func (h *APIv1Handler) serverState(w http.ResponseWriter, r *http.Request) {
	state := apiServerState{
		OK:        true,
		IsRunning: h.serverProcess.IsRunning(),
		Uptime:    time.Since(LaunchTime).String(),
		Version:   BuildVersion,
		GoVersion: runtime.Version(),
		OS:        runtime.GOOS + "/" + runtime.GOARCH,
		Now:       time.Now().UTC(),
	}

	if opts, err := h.store.LoadServerOptions(); err == nil {
		state.ServerName = opts.Name
	}

	if event := h.serverProcess.Event(); event != nil {
		cfg := event.GetRaceConfig()
		state.Track = cfg.Track
		state.TrackLayout = cfg.TrackLayout
		state.MaxClients = cfg.MaxClients
	}

	if h.raceControl != nil && h.raceControl.ConnectedDrivers != nil {
		state.NumDrivers = h.raceControl.ConnectedDrivers.Len()
		switch h.raceControl.SessionInfo.Type {
		case udp.SessionTypeRace:
			state.SessionType = "race"
		case udp.SessionTypeQualifying:
			state.SessionType = "qualifying"
		case udp.SessionTypePractice:
			state.SessionType = "practice"
		case udp.SessionTypeBooking:
			state.SessionType = "booking"
		}
	}

	writeAPIJSON(w, http.StatusOK, state)
}

// ---------- /api/v1/server/restart ----------

func (h *APIv1Handler) serverRestart(w http.ResponseWriter, r *http.Request) {
	if !h.serverProcess.IsRunning() {
		writeAPIError(w, http.StatusConflict, "server is not running; nothing to restart")
		return
	}

	if err := h.serverProcess.Restart(); err != nil {
		logrus.WithError(err).Error("api/v1: server restart failed")
		writeAPIError(w, http.StatusInternalServerError, "restart failed: "+err.Error())
		return
	}

	writeAPIJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---------- /api/v1/custom-races ----------

type apiCustomRace struct {
	UUID        string    `json:"uuid"`
	Name        string    `json:"name"`
	Track       string    `json:"track"`
	TrackLayout string    `json:"track_layout"`
	Cars        []string  `json:"cars"`
	Starred     bool      `json:"starred"`
	Looped      bool      `json:"looped"`
	Updated     time.Time `json:"updated"`
}

func splitCars(s string) []string {
	if s == "" {
		return []string{}
	}
	parts := strings.Split(s, ";")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func toAPICustomRace(c *CustomRace) apiCustomRace {
	return apiCustomRace{
		UUID:        c.UUID.String(),
		Name:        c.Name,
		Track:       c.RaceConfig.Track,
		TrackLayout: c.RaceConfig.TrackLayout,
		Cars:        splitCars(c.RaceConfig.Cars),
		Starred:     c.Starred,
		Looped:      c.IsLooping(),
		Updated:     c.Updated,
	}
}

func (h *APIv1Handler) listCustomRaces(w http.ResponseWriter, r *http.Request) {
	recent, _, _, _, err := h.raceManager.ListCustomRaces()
	if err != nil {
		logrus.WithError(err).Error("api/v1: list custom races failed")
		writeAPIError(w, http.StatusInternalServerError, "failed to list custom races")
		return
	}

	out := make([]apiCustomRace, 0, len(recent))
	for _, c := range recent {
		if c == nil {
			continue
		}
		out = append(out, toAPICustomRace(c))
	}

	writeAPIJSON(w, http.StatusOK, out)
}

func (h *APIv1Handler) loadCustomRace(w http.ResponseWriter, r *http.Request) {
	uuid := chi.URLParam(r, "uuid")
	if uuid == "" {
		writeAPIError(w, http.StatusBadRequest, "missing uuid")
		return
	}

	race, err := h.raceManager.StartCustomRace(uuid, false)
	if err != nil {
		logrus.WithError(err).Errorf("api/v1: failed to start custom race %s", uuid)
		writeAPIError(w, http.StatusInternalServerError, "failed to start custom race: "+err.Error())
		return
	}

	body := map[string]interface{}{"ok": true}
	if race != nil {
		body["race"] = toAPICustomRace(race)
	}
	writeAPIJSON(w, http.StatusOK, body)
}

// ---------- /api/v1/results ----------

type apiResultEntry struct {
	File string    `json:"file"`
	Date time.Time `json:"date"`
	Type string    `json:"type"`
}

func parseSessionTypeFromFilename(name string) string {
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	if i := strings.LastIndex(stem, "_"); i >= 0 {
		return stem[i+1:]
	}
	return ""
}

func (h *APIv1Handler) listResults(w http.ResponseWriter, r *http.Request) {
	since := int64(0)
	if v := r.URL.Query().Get("since"); v != "" {
		parsed, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid 'since' (expected unix milliseconds)")
			return
		}
		since = parsed
	}

	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		parsed, err := strconv.Atoi(v)
		if err != nil || parsed <= 0 || parsed > 500 {
			writeAPIError(w, http.StatusBadRequest, "invalid 'limit' (must be 1..500)")
			return
		}
		limit = parsed
	}

	resultsPath := filepath.Join(ServerInstallPath, "results")
	files, err := os.ReadDir(resultsPath)
	if err != nil {
		if os.IsNotExist(err) {
			writeAPIJSON(w, http.StatusOK, []apiResultEntry{})
			return
		}
		logrus.WithError(err).Error("api/v1: read results dir failed")
		writeAPIError(w, http.StatusInternalServerError, "failed to read results directory")
		return
	}

	type entry struct {
		name string
		date time.Time
	}
	all := make([]entry, 0, len(files))
	for _, f := range files {
		if f.IsDir() {
			continue
		}
		name := f.Name()
		if filepath.Ext(name) != ".json" {
			continue
		}
		d, err := GetResultDate(name)
		if err != nil {
			continue
		}
		if since > 0 && d.UnixNano()/int64(time.Millisecond) <= since {
			continue
		}
		all = append(all, entry{name: name, date: d})
	}

	sort.Slice(all, func(i, j int) bool { return all[i].date.After(all[j].date) })

	if len(all) > limit {
		all = all[:limit]
	}

	out := make([]apiResultEntry, 0, len(all))
	for _, e := range all {
		out = append(out, apiResultEntry{
			File: e.name,
			Date: e.date,
			Type: parseSessionTypeFromFilename(e.name),
		})
	}

	writeAPIJSON(w, http.StatusOK, out)
}

func (h *APIv1Handler) getResult(w http.ResponseWriter, r *http.Request) {
	file := chi.URLParam(r, "file")
	if file == "" {
		writeAPIError(w, http.StatusBadRequest, "missing file name")
		return
	}
	// path traversal guard
	if filepath.Base(file) != file {
		writeAPIError(w, http.StatusBadRequest, "invalid file name")
		return
	}

	result, err := LoadResult(file)
	if err != nil {
		if os.IsNotExist(err) {
			writeAPIError(w, http.StatusNotFound, "result not found")
			return
		}
		logrus.WithError(err).Errorf("api/v1: load result %s failed", file)
		writeAPIError(w, http.StatusInternalServerError, "failed to load result")
		return
	}

	writeAPIJSON(w, http.StatusOK, result)
}
