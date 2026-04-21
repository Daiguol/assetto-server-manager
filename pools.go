package servermanager

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

// PoolTrack holds a single track/layout entry inside a TrackCarPool.
type PoolTrack struct {
	Track  string `json:"track"`
	Layout string `json:"layout"`
	Name   string `json:"name"` // human-readable, e.g. "Interlagos GP"
}

// TrackCarPool groups a set of cars and circuits that can be referenced from the bot.
type TrackCarPool struct {
	ID      uuid.UUID   `json:"id"`
	Name    string      `json:"name"`
	Created time.Time   `json:"created"`
	Updated time.Time   `json:"updated"`
	Deleted time.Time   `json:"deleted"`
	Cars    []string    `json:"cars"`
	Tracks  []PoolTrack `json:"tracks"`
}

// PoolsHandler handles HTTP routes for TrackCarPool management.
type PoolsHandler struct {
	*BaseHandler
	store        Store
	carManager   *CarManager
	trackManager *TrackManager
}

func NewPoolsHandler(base *BaseHandler, store Store, carManager *CarManager, trackManager *TrackManager) *PoolsHandler {
	return &PoolsHandler{
		BaseHandler:  base,
		store:        store,
		carManager:   carManager,
		trackManager: trackManager,
	}
}

type poolsListTemplateVars struct {
	BaseTemplateVars
	Pools        []*TrackCarPool
	PoolUsedBy   map[uuid.UUID][]string // poolID → championship names
}

type poolEditTemplateVars struct {
	BaseTemplateVars
	Pool      *TrackCarPool
	IsNew     bool
	CarOpts   Cars
	TrackOpts TrackOptsGrouped
}

// list renders the list of all (non-deleted) pools, annotated with which championships use each.
func (ph *PoolsHandler) list(w http.ResponseWriter, r *http.Request) {
	pools, err := ph.store.ListTrackCarPools()
	if err != nil {
		logrus.WithError(err).Error("couldn't list pools")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	usedBy := make(map[uuid.UUID][]string)
	championships, err := ph.store.ListChampionships()
	if err != nil {
		logrus.WithError(err).Warn("couldn't list championships for pool usage annotation")
	} else {
		for _, c := range championships {
			for _, class := range c.Classes {
				if class.PoolID != uuid.Nil {
					usedBy[class.PoolID] = append(usedBy[class.PoolID], c.Name)
				}
			}
		}
	}

	ph.viewRenderer.MustLoadTemplate(w, r, "content/pools.html", &poolsListTemplateVars{
		Pools:      pools,
		PoolUsedBy: usedBy,
	})
}

// createOrEdit renders the pool creation/edit form.
func (ph *PoolsHandler) createOrEdit(w http.ResponseWriter, r *http.Request) {
	poolID := chi.URLParam(r, "poolID")
	isNew := poolID == ""

	var pool *TrackCarPool
	if isNew {
		pool = &TrackCarPool{ID: uuid.New(), Created: time.Now()}
	} else {
		var err error
		pool, err = ph.store.LoadTrackCarPool(poolID)
		if err != nil {
			logrus.WithError(err).Error("couldn't load pool")
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
	}

	cars, err := ph.carManager.ListCars()
	if err != nil {
		logrus.WithError(err).Error("couldn't list cars for pool form")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	tracks, err := ph.trackManager.ListTracks()
	if err != nil {
		logrus.WithError(err).Error("couldn't list tracks for pool form")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	ph.viewRenderer.MustLoadTemplate(w, r, "content/pool-edit.html", &poolEditTemplateVars{
		Pool:      pool,
		IsNew:     isNew,
		CarOpts:   cars,
		TrackOpts: TrackOptsGrouped(tracks),
	})
}

// submit handles POST for both new and existing pools.
func (ph *PoolsHandler) submit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	poolID := chi.URLParam(r, "poolID")
	isNew := poolID == ""

	var pool *TrackCarPool
	if isNew {
		pool = &TrackCarPool{ID: uuid.New(), Created: time.Now()}
	} else {
		var err error
		pool, err = ph.store.LoadTrackCarPool(poolID)
		if err != nil {
			logrus.WithError(err).Error("couldn't load pool for submit")
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
	}

	pool.Name = r.FormValue("PoolName")
	pool.Cars = r.Form["PoolCars"]

	trackIDs := r.Form["PoolTrack"]
	layouts := r.Form["PoolLayout"]
	names := r.Form["PoolTrackName"]

	pool.Tracks = nil
	for i, track := range trackIDs {
		if track == "" {
			continue
		}
		pt := PoolTrack{Track: track}
		if i < len(layouts) {
			pt.Layout = layouts[i]
		}
		if i < len(names) {
			pt.Name = names[i]
		}
		pool.Tracks = append(pool.Tracks, pt)
	}

	if err := ph.store.UpsertTrackCarPool(pool); err != nil {
		logrus.WithError(err).Error("couldn't save pool")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/pools", http.StatusFound)
}

// delete soft-deletes the pool by ID, blocking if any championship class still references it.
func (ph *PoolsHandler) delete(w http.ResponseWriter, r *http.Request) {
	poolID := chi.URLParam(r, "poolID")

	parsedID, err := uuid.Parse(poolID)
	if err != nil {
		http.Error(w, "invalid pool ID", http.StatusBadRequest)
		return
	}

	championships, err := ph.store.ListChampionships()
	if err != nil {
		logrus.WithError(err).Error("couldn't list championships for pool delete check")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	for _, c := range championships {
		for _, class := range c.Classes {
			if class.PoolID == parsedID {
				AddErrorFlash(w, r, "Cannot delete pool: it is used by championship \""+c.Name+"\" (class \""+class.Name+"\"). Remove it from the championship first.")
				http.Redirect(w, r, "/pools", http.StatusFound)
				return
			}
		}
	}

	if err := ph.store.DeleteTrackCarPool(poolID); err != nil {
		logrus.WithError(err).Error("couldn't delete pool")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/pools", http.StatusFound)
}

// apiList returns all pools as JSON. Used by the bot.
func (ph *PoolsHandler) apiList(w http.ResponseWriter, r *http.Request) {
	pools, err := ph.store.ListTrackCarPools()
	if err != nil {
		logrus.WithError(err).Error("couldn't list pools for API")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(pools); err != nil {
		logrus.WithError(err).Error("couldn't encode pools JSON")
	}
}
