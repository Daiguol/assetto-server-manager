package servermanager

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"4d63.com/tz"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

type ChampionshipsHandler struct {
	*BaseHandler
	SteamLoginHandler

	championshipManager *ChampionshipManager
}

func NewChampionshipsHandler(baseHandler *BaseHandler, championshipManager *ChampionshipManager) *ChampionshipsHandler {
	return &ChampionshipsHandler{
		BaseHandler:         baseHandler,
		championshipManager: championshipManager,
	}
}

type championshipListTemplateVars struct {
	BaseTemplateVars

	ActiveChampionships, CompletedChampionships []*Championship
}

// lists all available Championships known to Server Manager
func (ch *ChampionshipsHandler) list(w http.ResponseWriter, r *http.Request) {
	championships, err := ch.championshipManager.ListChampionships()

	if err != nil {
		logrus.WithError(err).Errorf("couldn't list championships")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	var activeChampionships, completedChampionships []*Championship

	for _, championship := range championships {
		if championship.Progress() == 100 {
			completedChampionships = append(completedChampionships, championship)
		} else {
			activeChampionships = append(activeChampionships, championship)
		}
	}

	ch.viewRenderer.MustLoadTemplate(w, r, "championships/index.html", &championshipListTemplateVars{
		ActiveChampionships:    activeChampionships,
		CompletedChampionships: completedChampionships,
	})
}

// createOrEdit builds a Championship form for the user to create a Championship.
func (ch *ChampionshipsHandler) createOrEdit(w http.ResponseWriter, r *http.Request) {
	_, opts, err := ch.championshipManager.BuildChampionshipOpts(r)

	if err != nil {
		logrus.WithError(err).Errorf("couldn't build championship form")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	ch.viewRenderer.MustLoadTemplate(w, r, "championships/new.html", opts)
}

// submit creates a given Championship and redirects the user to begin
// the flow of adding events to the new Championship
func (ch *ChampionshipsHandler) submit(w http.ResponseWriter, r *http.Request) {
	championship, edited, err := ch.championshipManager.HandleCreateChampionship(r)

	if err != nil {
		logrus.WithError(err).Errorf("couldn't create championship")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	if edited {
		AddFlash(w, r, "Championship successfully edited!")
		http.Redirect(w, r, "/championship/"+championship.ID.String(), http.StatusFound)
	} else {

		AddFlash(w, r, "We've created the Championship. Now you need to add some Events!")

		if r.FormValue("action") == "addRaceWeekend" {
			http.Redirect(w, r, "/race-weekends/new?championshipID="+championship.ID.String(), http.StatusFound)
		} else {
			http.Redirect(w, r, "/championship/"+championship.ID.String()+"/event", http.StatusFound)
		}
	}
}

// duplicate duplicates a championship, clearing results
func (ch *ChampionshipsHandler) duplicate(w http.ResponseWriter, r *http.Request) {
	championship, err := ch.championshipManager.DuplicateChampionship(chi.URLParam(r, "championshipID"))

	if err != nil {
		logrus.WithError(err).Errorf("couldn't duplicate championship (id: %s)", chi.URLParam(r, "championshipID"))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	AddFlash(w, r, "Championship successfully duplicated!")
	http.Redirect(w, r, "/championship/"+championship.ID.String(), http.StatusFound)
}

type championshipViewTemplateVars struct {
	BaseTemplateVars

	Championship    *Championship
	EventInProgress bool
	Account         *Account
	RaceWeekends    map[uuid.UUID]*RaceWeekend
	DriverRatings   map[string]*ACSRDriverRating
	AccountRating   *ACSRDriverRating
}

// view shows details of a given Championship
func (ch *ChampionshipsHandler) view(w http.ResponseWriter, r *http.Request) {
	championship, err := ch.championshipManager.LoadChampionship(chi.URLParam(r, "championshipID"))

	if err != nil {
		logrus.WithError(err).Errorf("couldn't load championship")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	eventInProgress := false

	for _, event := range championship.Events {
		if event.InProgress() && ch.championshipManager.activeChampionship != nil {
			eventInProgress = true
			break
		}
	}

	ratings, err := ch.championshipManager.LoadACSRRatings(championship)

	if err != nil {
		logrus.WithError(err).Error("could not get driver ratings from ACSR")
	}

	raceWeekends := make(map[uuid.UUID]*RaceWeekend)

	for _, event := range championship.Events {
		if event.IsRaceWeekend() {
			raceWeekends[event.RaceWeekendID], err = ch.championshipManager.store.LoadRaceWeekend(event.RaceWeekendID.String())

			if err != nil {
				logrus.WithError(err).Errorf("couldn't load championship")
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				return
			}
		}
	}

	var rating *ACSRDriverRating
	account := AccountFromRequest(r)

	if account.GUID != "" {
		rating, err = ch.championshipManager.LoadACSRRating(account.GUID)

		if err != nil {
			logrus.WithError(err).Errorf("Couldn't load ACSR rating for guid: %s", account.GUID)
		}
	}

	ch.viewRenderer.MustLoadTemplate(w, r, "championships/view.html", &championshipViewTemplateVars{
		Championship:    championship,
		EventInProgress: eventInProgress,
		Account:         account,
		RaceWeekends:    raceWeekends,
		DriverRatings:   ratings,
		AccountRating:   rating,
	})
}

type ACSRRatingGateMet struct {
	ACSRDriverRating *ACSRDriverRating `json:"acsr_driver_rating"`
	GateMet          bool              `json:"gate_met"`
	ACSREnabled      bool              `json:"acsr_enabled"`
}

func (ch *ChampionshipsHandler) acsrRating(w http.ResponseWriter, r *http.Request) {
	serverOptions, err := ch.championshipManager.store.LoadServerOptions()

	if err != nil {
		logrus.WithError(err).Error("acsr rating: couldn't load server options")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	if !serverOptions.EnableACSR {
		ratingGateMet := &ACSRRatingGateMet{
			ACSRDriverRating: &ACSRDriverRating{},
			GateMet:          true,
			ACSREnabled:      false,
		}

		_ = json.NewEncoder(w).Encode(ratingGateMet)

		return
	}

	guid := chi.URLParam(r, "guid")
	championshipID := chi.URLParam(r, "championshipID")

	championship, err := ch.championshipManager.LoadChampionship(championshipID)

	if err != nil {
		logrus.WithError(err).Errorf("acsr rating: couldn't load championship for ID: %s", championshipID)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	rating, err := ch.championshipManager.LoadACSRRating(guid)

	if err != nil {
		logrus.WithError(err).Errorf("Couldn't load ACSR rating for guid: %s", guid)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	gateMet := championship.DriverMeetsACSRGates(rating)

	ratingGateMet := &ACSRRatingGateMet{
		ACSRDriverRating: rating,
		GateMet:          gateMet,
		ACSREnabled:      true,
	}

	_ = json.NewEncoder(w).Encode(ratingGateMet)
}

// export returns all known data about a Championship in JSON format.
func (ch *ChampionshipsHandler) export(w http.ResponseWriter, r *http.Request) {
	championship, err := ch.championshipManager.LoadChampionship(chi.URLParam(r, "championshipID"))

	if err != nil {
		logrus.WithError(err).Errorf("couldn't export championship")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	account := AccountFromRequest(r)

	if !account.HasGroupPrivilege(GroupAdmin) {
		// sign up responses are hidden for data protection reasons
		championship.SignUpForm.Responses = nil
	}

	if !account.HasGroupPrivilege(GroupWrite) {
		// if you don't have write access or above you can't see the replacement password
		championship.ReplacementPassword = ""
		championship.OverridePassword = false
	}

	w.Header().Add("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.json"`, championship.Name))

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(championship)
}

// importChampionship reads Championship data from JSON.
func (ch *ChampionshipsHandler) importChampionship(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		championshipID, err := ch.championshipManager.ImportChampionship(r.FormValue("import"))

		if err != nil {
			logrus.WithError(err).Errorf("couldn't import championship")
			AddErrorFlash(w, r, "Sorry, we couldn't import that championship! Check your JSON formatting.")
		} else {
			AddFlash(w, r, "Championship successfully imported!")
			http.Redirect(w, r, "/championship/"+championshipID, http.StatusFound)
		}
	}

	ch.viewRenderer.MustLoadTemplate(w, r, "championships/import-championship.html", nil)
}

type championshipResultsCollection struct {
	Name    string                `json:"name"`
	Results []championshipResults `json:"results"`
}

type championshipResults struct {
	Name string   `json:"name"`
	Log  []string `json:"log"`
}

// exportResults returns championship result files in JSON format.
func (ch *ChampionshipsHandler) exportResults(w http.ResponseWriter, r *http.Request) {
	championship, err := ch.championshipManager.LoadChampionship(chi.URLParam(r, "championshipID"))

	if err != nil {
		logrus.WithError(err).Errorf("couldn't export championship")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	var results []championshipResults

	for _, event := range championship.Events {

		if !event.Completed() {
			continue
		}

		var sessionFiles []string

		for _, session := range event.Sessions {
			if session.Results == nil {
				continue
			}

			sessionFiles = append(sessionFiles, session.Results.GetURL())
		}

		results = append(results, championshipResults{
			Name: "Event at " + prettifyName(event.RaceSetup.Track, false) + ", completed on " + event.CompletedTime.Format("Monday, January 2, 2006 3:04 PM (MST)"),
			Log:  sessionFiles,
		})
	}

	champResultsCollection := championshipResultsCollection{
		Name:    championship.Name,
		Results: results,
	}

	w.Header().Add("Content-Type", "application/json")

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(champResultsCollection)
}

// delete soft deletes a Championship.
func (ch *ChampionshipsHandler) delete(w http.ResponseWriter, r *http.Request) {
	err := ch.championshipManager.DeleteChampionship(chi.URLParam(r, "championshipID"))

	if err != nil {
		logrus.WithError(err).Errorf("couldn't delete championship")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	AddFlash(w, r, "Championship deleted!")
	http.Redirect(w, r, r.Referer(), http.StatusFound)
}

type eventImportTemplateVars struct {
	BaseTemplateVars

	Results        []SessionResults
	ChampionshipID string
	Event          *ChampionshipEvent
}

func (ch *ChampionshipsHandler) eventImport(w http.ResponseWriter, r *http.Request) {
	championshipID := chi.URLParam(r, "championshipID")
	eventID := chi.URLParam(r, "eventID")

	if r.Method == http.MethodPost {
		err := ch.championshipManager.ImportEvent(championshipID, eventID, r)

		if err != nil {
			logrus.Errorf("Could not import championship event, error: %s", err)
			AddErrorFlash(w, r, "Could not import session files")
		} else {
			AddFlash(w, r, "Successfully imported session files!")
			http.Redirect(w, r, "/championship/"+championshipID, http.StatusFound)
			return
		}
	}

	event, results, err := ch.championshipManager.ListAvailableResultsFilesForEvent(championshipID, eventID)

	if err != nil {
		logrus.WithError(err).Errorf("Couldn't load session files")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	ch.viewRenderer.MustLoadTemplate(w, r, "championships/import-event.html", &eventImportTemplateVars{
		Results:        results,
		ChampionshipID: championshipID,
		Event:          event,
	})
}

// eventConfiguration builds a Custom Race form with slight modifications
// to allow a user to configure a ChampionshipEvent.
func (ch *ChampionshipsHandler) eventConfiguration(w http.ResponseWriter, r *http.Request) {
	championshipRaceOpts, err := ch.championshipManager.BuildChampionshipEventOpts(r)

	if err != nil {
		logrus.WithError(err).Errorf("couldn't build championship race")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	ch.viewRenderer.MustLoadTemplate(w, r, "custom-race/new.html", championshipRaceOpts)
}

// submitEventConfiguration takes an Event Configuration from a form and
// builds an event optionally, this is used for editing ChampionshipEvents.
func (ch *ChampionshipsHandler) submitEventConfiguration(w http.ResponseWriter, r *http.Request) {
	championship, event, edited, err := ch.championshipManager.SaveChampionshipEvent(r)

	if err != nil {
		logrus.WithError(err).Errorf("couldn't build championship race")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	if edited {
		AddFlash(w, r,
			fmt.Sprintf(
				"Championship race at %s was successfully edited!",
				prettifyName(event.RaceSetup.Track, false),
			),
		)
	} else {
		AddFlash(w, r,
			fmt.Sprintf(
				"Championship race at %s was successfully added!",
				prettifyName(event.RaceSetup.Track, false),
			),
		)
	}

	if r.FormValue("action") == "saveChampionship" {
		// end the race creation flow
		http.Redirect(w, r, "/championship/"+championship.ID.String(), http.StatusFound)
		return
	}

	// add another event
	http.Redirect(w, r, "/championship/"+championship.ID.String()+"/event", http.StatusFound)
}

// startEvent begins a championship event given by its ID
func (ch *ChampionshipsHandler) startEvent(w http.ResponseWriter, r *http.Request) {
	err := ch.championshipManager.StartEvent(chi.URLParam(r, "championshipID"), chi.URLParam(r, "eventID"), false)

	if err != nil {
		logrus.WithError(err).Errorf("Could not start championship event")

		AddErrorFlash(w, r, "Couldn't start the Event")
	} else {
		AddFlash(w, r, "Event started successfully!")
		time.Sleep(time.Second * 1)
	}

	http.Redirect(w, r, r.Referer(), http.StatusFound)
}

func (ch *ChampionshipsHandler) scheduleEvent(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		logrus.WithError(err).Errorf("couldn't parse schedule race form")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	championshipID := chi.URLParam(r, "championshipID")
	championshipEventID := chi.URLParam(r, "eventID")
	dateString := r.FormValue("event-schedule-date")
	timeString := r.FormValue("event-schedule-time")
	timezone := r.FormValue("event-schedule-timezone")

	location, err := tz.LoadLocation(timezone)

	if err != nil {
		logrus.WithError(err).Errorf("could not find location: %s", location)
		location = time.Local
	}

	// Parse time in correct time zone
	date, err := time.ParseInLocation("2006-01-02-15:04", dateString+"-"+timeString, location)

	if err != nil {
		logrus.WithError(err).Errorf("couldn't parse schedule championship event date")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	err = ch.championshipManager.ScheduleEvent(championshipID, championshipEventID, date, r.FormValue("action"), r.FormValue("event-schedule-recurrence"))

	if err != nil {
		logrus.WithError(err).Errorf("couldn't schedule championship event")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	AddFlash(w, r, fmt.Sprintf("We have scheduled the Championship Event to begin at %s", date.Format(time.RFC1123)))
	http.Redirect(w, r, r.Referer(), http.StatusFound)
}

func (ch *ChampionshipsHandler) scheduleEventRemove(w http.ResponseWriter, r *http.Request) {
	err := ch.championshipManager.ScheduleEvent(chi.URLParam(r, "championshipID"), chi.URLParam(r, "eventID"),
		time.Time{}, "remove", "")

	if err != nil {
		logrus.WithError(err).Errorf("couldn't schedule championship event")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, r.Referer(), http.StatusFound)
}

// deleteEvent soft deletes a championship event
func (ch *ChampionshipsHandler) deleteEvent(w http.ResponseWriter, r *http.Request) {
	err := ch.championshipManager.DeleteEvent(chi.URLParam(r, "championshipID"), chi.URLParam(r, "eventID"))

	if err != nil {
		logrus.WithError(err).Errorf("Could not delete championship event")

		AddErrorFlash(w, r, "Couldn't delete the Event")
	} else {
		AddFlash(w, r, "Event deleted successfully!")
	}

	http.Redirect(w, r, r.Referer(), http.StatusFound)
}

// startPracticeEvent starts a Practice session for a given event
func (ch *ChampionshipsHandler) startPracticeEvent(w http.ResponseWriter, r *http.Request) {
	err := ch.championshipManager.StartPracticeEvent(chi.URLParam(r, "championshipID"), chi.URLParam(r, "eventID"))

	if err != nil {
		logrus.WithError(err).Errorf("Could not start practice championship event")

		AddErrorFlash(w, r, "Couldn't start the Practice Event")
	} else {
		AddFlash(w, r, "Practice Event started successfully!")
		time.Sleep(time.Second * 1)
	}

	http.Redirect(w, r, r.Referer(), http.StatusFound)
}

// cancelEvent stops a running championship event and clears any saved results
func (ch *ChampionshipsHandler) cancelEvent(w http.ResponseWriter, r *http.Request) {
	err := ch.championshipManager.CancelEvent(chi.URLParam(r, "championshipID"), chi.URLParam(r, "eventID"))

	if err != nil {
		logrus.WithError(err).Errorf("Could not cancel championship event")

		AddErrorFlash(w, r, "Couldn't cancel the Championship Event")
	} else {
		AddFlash(w, r, "Event cancelled successfully!")
	}

	http.Redirect(w, r, r.Referer(), http.StatusFound)
}

// restartEvent stops a running championship event and clears any saved results
// then starts the event again.
func (ch *ChampionshipsHandler) restartEvent(w http.ResponseWriter, r *http.Request) {
	err := ch.championshipManager.RestartEvent(chi.URLParam(r, "championshipID"), chi.URLParam(r, "eventID"))

	if err != nil {
		logrus.WithError(err).Errorf("Could not restart championship event")

		AddErrorFlash(w, r, "Couldn't restart the Championship Event")
	} else {
		AddFlash(w, r, "Event restarted successfully!")
	}

	http.Redirect(w, r, r.Referer(), http.StatusFound)
}

type championshipCustomRaceImportTemplateVars struct {
	BaseTemplateVars

	Recent, Starred, Loop, Scheduled []*CustomRace
	Championship                     *Championship
}

type championshipRaceWeekendImportTemplateVars struct {
	BaseTemplateVars

	RaceWeekends []*RaceWeekend
	Championship *Championship
}

func (ch *ChampionshipsHandler) listCustomRacesForImport(w http.ResponseWriter, r *http.Request) {
	recent, starred, looped, scheduled, err := ch.championshipManager.RaceManager.ListCustomRaces()

	if err != nil {
		logrus.WithError(err).Errorf("couldn't list custom races")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	championship, err := ch.championshipManager.LoadChampionship(chi.URLParam(r, "championshipID"))

	if err != nil {
		logrus.WithError(err).Errorf("couldn't load championship for custom race import page list")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	ch.viewRenderer.MustLoadTemplate(w, r, "championships/import-custom.html", &championshipCustomRaceImportTemplateVars{
		Recent:       recent,
		Starred:      starred,
		Loop:         looped,
		Scheduled:    scheduled,
		Championship: championship,
	})
}

func (ch *ChampionshipsHandler) customRaceImport(w http.ResponseWriter, r *http.Request) {
	championshipID := chi.URLParam(r, "championshipID")

	err := ch.championshipManager.ImportEventSetup(championshipID, chi.URLParam(r, "eventID"))

	if err != nil {
		logrus.WithError(err).Error("could not import event setup to championship")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/championship/"+championshipID, http.StatusFound)
}

func (ch *ChampionshipsHandler) listRaceWeekendsForImport(w http.ResponseWriter, r *http.Request) {
	raceWeekends, err := ch.championshipManager.store.ListRaceWeekends()

	if err != nil {
		logrus.WithError(err).Errorf("couldn't list race weekends for championship import")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	championship, err := ch.championshipManager.LoadChampionship(chi.URLParam(r, "championshipID"))

	if err != nil {
		logrus.WithError(err).Errorf("couldn't load championship for custom race import page list")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	ch.viewRenderer.MustLoadTemplate(w, r, "championships/import-weekend.html", &championshipRaceWeekendImportTemplateVars{
		RaceWeekends: raceWeekends,
		Championship: championship,
	})
}

func (ch *ChampionshipsHandler) raceWeekendImport(w http.ResponseWriter, r *http.Request) {
	championshipID := chi.URLParam(r, "championshipID")

	err := ch.championshipManager.ImportRaceWeekendSetup(championshipID, chi.URLParam(r, "weekendID"))

	if err != nil {
		logrus.WithError(err).Error("could not import race weekend setup to championship")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/championship/"+championshipID, http.StatusFound)
}

func (ch *ChampionshipsHandler) icalFeed(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Add("Content-Disposition", "inline; filename=championship.ics")

	err := ch.championshipManager.BuildICalFeed(chi.URLParam(r, "championshipID"), w)

	if err != nil {
		logrus.WithError(err).Error("could not build scheduled races feed")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
}

func (ch *ChampionshipsHandler) driverPenalty(w http.ResponseWriter, r *http.Request) {
	err := ch.championshipManager.ModifyDriverPenalty(
		chi.URLParam(r, "championshipID"),
		chi.URLParam(r, "classID"),
		chi.URLParam(r, "driverGUID"),
		PenaltyAction(r.FormValue("action")),
		formValueAsInt(r.FormValue("PointsPenalty")),
	)

	if err != nil {
		logrus.WithError(err).Errorf("Could not modify championship driver penalty")

		AddErrorFlash(w, r, "Couldn't modify driver penalty")
	} else {
		AddFlash(w, r, "Driver penalty successfully modified")
	}

	http.Redirect(w, r, r.Referer(), http.StatusFound)
}

func (ch *ChampionshipsHandler) teamPenalty(w http.ResponseWriter, r *http.Request) {
	err := ch.championshipManager.ModifyTeamPenalty(
		chi.URLParam(r, "championshipID"),
		chi.URLParam(r, "classID"),
		chi.URLParam(r, "team"),
		PenaltyAction(r.FormValue("action")),
		formValueAsInt(r.FormValue("PointsPenalty")),
	)

	if err != nil {
		logrus.WithError(err).Errorf("Could not modify championship penalty")

		AddErrorFlash(w, r, "Couldn't modify team penalty")
	} else {
		AddFlash(w, r, "Team penalty successfully modified")
	}

	http.Redirect(w, r, r.Referer(), http.StatusFound)
}

type championshipSignUpFormTemplateVars struct {
	*ChampionshipTemplateVars

	FormData        *ChampionshipSignUpResponse
	ValidationError string
	LockSteamGUID   bool
}

func (ch *ChampionshipsHandler) signUpForm(w http.ResponseWriter, r *http.Request) {
	championship, championshipOpts, err := ch.championshipManager.BuildChampionshipOpts(r)

	if err != nil {
		logrus.WithError(err).Error("couldn't load championship")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	opts := &championshipSignUpFormTemplateVars{
		ChampionshipTemplateVars: championshipOpts,
	}

	if !championship.SignUpAvailable() {
		http.NotFound(w, r)
		return
	}

	account := AccountFromRequest(r)

	if account != OpenAccount {
		opts.FormData = &ChampionshipSignUpResponse{
			Name: account.DriverName,
			GUID: account.GUID,
			Team: account.Team,
		}
	} else {
		opts.FormData = &ChampionshipSignUpResponse{}
	}

	if r.Method == http.MethodPost {
		signUpResponse, foundSlot, err := ch.championshipManager.HandleChampionshipSignUp(r)

		if err != nil {
			switch err.(type) {
			case ValidationError:
				opts.FormData = signUpResponse
				opts.ValidationError = err.Error()
			default:
				logrus.WithError(err).Error("couldn't handle championship")
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				return
			}
		} else {
			if championship.SignUpForm.RequiresApproval {
				AddFlash(w, r, "Thanks for registering for the championship! Your registration is pending approval by an administrator.")
				http.Redirect(w, r, "/championship/"+championship.ID.String(), http.StatusFound)
				return
			}

			if foundSlot {
				AddFlash(w, r, "Thanks for registering for the championship!")
				http.Redirect(w, r, "/championship/"+championship.ID.String(), http.StatusFound)
				return
			}

			opts.FormData = signUpResponse
			opts.ValidationError = fmt.Sprintf("There are no more available slots for the car: %s. Please pick a different car.", prettifyName(signUpResponse.GetCar(), true))
		}
	}

	if steamGUID := r.URL.Query().Get("steamGUID"); steamGUID != "" {
		opts.FormData.GUID = steamGUID
		opts.LockSteamGUID = true
	}

	ch.viewRenderer.MustLoadTemplate(w, r, "championships/sign-up.html", opts)
}

type signedUpEntrantsTemplateVars struct {
	BaseTemplateVars

	Championship  *Championship
	DriverRatings map[string]*ACSRDriverRating
}

func (ch *ChampionshipsHandler) signedUpEntrants(w http.ResponseWriter, r *http.Request) {
	championship, err := ch.championshipManager.LoadChampionship(chi.URLParam(r, "championshipID"))

	if err != nil {
		logrus.WithError(err).Error("couldn't load championship")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	if !championship.SignUpForm.Enabled {
		http.NotFound(w, r)
		return
	}

	ratings, err := ch.championshipManager.LoadACSRRatings(championship)

	if err != nil {
		logrus.WithError(err).Error("couldn't load ratings from ACSR")
	}

	sort.Slice(championship.SignUpForm.Responses, func(i, j int) bool {
		return championship.SignUpForm.Responses[i].Created.After(championship.SignUpForm.Responses[j].Created)
	})

	ch.viewRenderer.MustLoadTemplate(w, r, "championships/signed-up-entrants.html", &signedUpEntrantsTemplateVars{
		BaseTemplateVars: BaseTemplateVars{WideContainer: true},
		Championship:     championship,
		DriverRatings:    ratings,
	})
}

func (ch *ChampionshipsHandler) signedUpEntrantsCSV(w http.ResponseWriter, r *http.Request) {
	championship, err := ch.championshipManager.LoadChampionship(chi.URLParam(r, "championshipID"))

	if err != nil {
		logrus.WithError(err).Error("couldn't load championship")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	headers := []string{
		"Created",
		"Name",
		"Team",
		"GUID",
		"Email",
		"Car",
		"Skin",
		"Status",
	}

	headers = append(headers, championship.SignUpForm.ExtraFields...)

	ratings, err := ch.championshipManager.LoadACSRRatings(championship)

	if err != nil {
		logrus.WithError(err).Error("couldn't load ratings from ACSR")
	}

	if ratings != nil {
		headers = append(headers, "ACSR Skill Rating", "ACSR Safety Rating", "ACSR Provisional?")
	}

	var out [][]string

	out = append(out, headers)

	for _, entrant := range championship.SignUpForm.Responses {
		data := []string{
			entrant.Created.String(),
			entrant.Name,
			entrant.Team,
			entrant.GUID,
			entrant.Email,
			entrant.Car,
			entrant.Skin,
			string(entrant.Status),
		}

		for _, question := range championship.SignUpForm.ExtraFields {
			if response, ok := entrant.Questions[question]; ok {
				data = append(data, response)
			} else {
				data = append(data, "")
			}
		}

		if ratings != nil {
			if rating, ok := ratings[entrant.GUID]; ok {
				data = append(data, rating.SkillRatingGrade, strconv.Itoa(rating.SafetyRating), strconv.FormatBool(rating.IsProvisional))
			} else {
				data = append(data, "Unranked", "Unranked", "Unranked")
			}
		}

		out = append(out, data)
	}

	w.Header().Add("Content-Type", "text/csv")
	w.Header().Add("Content-Disposition", fmt.Sprintf(`attachment;filename="Entrants_%s.csv"`, championship.Name))
	wr := csv.NewWriter(w)
	wr.UseCRLF = true
	_ = wr.WriteAll(out)
}

func (ch *ChampionshipsHandler) modifyEntrantStatus(w http.ResponseWriter, r *http.Request) {
	championship, err := ch.championshipManager.LoadChampionship(chi.URLParam(r, "championshipID"))

	if err != nil {
		logrus.WithError(err).Error("couldn't load championship")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	if !championship.SignUpForm.Enabled {
		http.NotFound(w, r)
		return
	}

	entrantGUID := chi.URLParam(r, "entrantGUID")

	for index, entrant := range championship.SignUpForm.Responses {
		if entrant.GUID != entrantGUID {
			continue
		}

		switch r.URL.Query().Get("action") {
		case "accept":
			if entrant.Status == ChampionshipEntrantAccepted {
				AddFlash(w, r, "This entrant has already been accepted.")
				break
			}

			// add the entrant to the entrylist
			foundSlot, _, err := ch.championshipManager.AddEntrantFromSessionData(championship, entrant, true, championship.SignUpForm.HideCarChoice)

			if err != nil {
				logrus.WithError(err).Error("couldn't add entrant to championship")
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				return
			}

			if foundSlot {
				entrant.Status = ChampionshipEntrantAccepted

				AddFlash(w, r, "The entrant was successfully accepted!")
			} else {
				AddErrorFlash(w, r, "There are no more slots available for the given entrant and car. Please check the Championship configuration.")
			}
		case "reject":
			entrant.Status = ChampionshipEntrantRejected
			championship.ClearEntrant(entrantGUID)
		case "delete":
			championship.SignUpForm.Responses = append(championship.SignUpForm.Responses[:index], championship.SignUpForm.Responses[index+1:]...)
			championship.ClearEntrant(entrantGUID)

		default:
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
	}

	if err := ch.championshipManager.UpsertChampionship(championship); err != nil {
		logrus.WithError(err).Error("couldn't save championship")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, r.Referer(), http.StatusFound)
}

func (ch *ChampionshipsHandler) reorderEvents(w http.ResponseWriter, r *http.Request) {
	var eventIDsInOrder []string

	if err := json.NewDecoder(r.Body).Decode(&eventIDsInOrder); err != nil {
		logrus.WithError(err).Error("couldn't parse event reorder request")
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	if err := ch.championshipManager.ReorderChampionshipEvents(chi.URLParam(r, "championshipID"), eventIDsInOrder); err != nil {
		logrus.WithError(err).Error("couldn't reorder championship events")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
}

func (ch *ChampionshipsHandler) duplicateEvent(w http.ResponseWriter, r *http.Request) {
	championshipID := chi.URLParam(r, "championshipID")
	eventID := chi.URLParam(r, "eventID")

	newEvent, err := ch.championshipManager.DuplicateEventInChampionship(championshipID, eventID)

	if err != nil {
		logrus.WithError(err).Error("couldn't duplicate championship race weekend")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	if newEvent.IsRaceWeekend() {
		AddFlash(w, r, "Championship Race Weekend was successfully duplicated!")
		http.Redirect(w, r, "/race-weekend/"+newEvent.RaceWeekendID.String(), http.StatusFound)
	} else {
		AddFlash(w, r, "Championship Event was successfully duplicated!")
		http.Redirect(w, r, "/championship/"+championshipID+"/event/"+newEvent.ID.String()+"/edit", http.StatusFound)
	}
}

// apiChampionshipEvents returns all events of a championship as JSON.
// GET /api/championship/{championshipID}/events
func (ch *ChampionshipsHandler) apiChampionshipEvents(w http.ResponseWriter, r *http.Request) {
	championship, err := ch.championshipManager.LoadChampionship(chi.URLParam(r, "championshipID"))
	if err != nil {
		logrus.WithError(err).Error("couldn't load championship for API")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	type eventDTO struct {
		ID        string `json:"id"`
		Index     int    `json:"index"`
		Track     string `json:"track"`
		Layout    string `json:"layout"`
		Started   bool   `json:"started"`
		Completed bool   `json:"completed"`
		ClassID   string `json:"classId"`
		ClassName string `json:"className"`
	}

	// build classID → name map
	classNames := make(map[string]string)
	for _, cl := range championship.Classes {
		classNames[cl.ID.String()] = cl.Name
	}

	events := make([]eventDTO, 0, len(championship.Events))
	for i, ev := range championship.Events {
		classIDStr := ev.ClassID.String()
		if ev.ClassID == uuid.Nil {
			classIDStr = ""
		}
		events = append(events, eventDTO{
			ID:        ev.ID.String(),
			Index:     i,
			Track:     ev.RaceSetup.Track,
			Layout:    ev.RaceSetup.TrackLayout,
			Started:   ev.InProgress() || ev.Completed(),
			Completed: ev.Completed(),
			ClassID:   classIDStr,
			ClassName: classNames[classIDStr],
		})
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(events); err != nil {
		logrus.WithError(err).Error("couldn't encode events JSON")
	}
}

// apiStartChampionshipEvent starts a championship event and responds with JSON.
// POST /api/championship/{championshipID}/event/{eventID}/start
func (ch *ChampionshipsHandler) apiStartChampionshipEvent(w http.ResponseWriter, r *http.Request) {
	championshipID := chi.URLParam(r, "championshipID")
	eventID := chi.URLParam(r, "eventID")

	err := ch.championshipManager.StartEvent(championshipID, eventID, false)

	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		logrus.WithError(err).Error("couldn't start championship event via API")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

// apiListChampionships returns active (non-completed) championships as JSON.
// GET /api/championships
func (ch *ChampionshipsHandler) apiListChampionships(w http.ResponseWriter, r *http.Request) {
	championships, err := ch.championshipManager.ListChampionships()
	if err != nil {
		logrus.WithError(err).Error("couldn't list championships for API")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	type championshipDTO struct {
		ID       string  `json:"id"`
		Name     string  `json:"name"`
		Progress float64 `json:"progress"`
	}

	var result []championshipDTO
	for _, c := range championships {
		if c.Progress() < 100 {
			result = append(result, championshipDTO{
				ID:       c.ID.String(),
				Name:     c.Name,
				Progress: c.Progress(),
			})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(result); err != nil {
		logrus.WithError(err).Error("couldn't encode championships JSON")
	}
}

// randomEventsConfig shows the race configuration form before generating random events.
// GET /championship/{championshipID}/random-events
func (ch *ChampionshipsHandler) randomEventsConfig(w http.ResponseWriter, r *http.Request) {
	championshipID := chi.URLParam(r, "championshipID")

	opts, err := ch.championshipManager.BuildChampionshipEventOpts(r)
	if err != nil {
		logrus.WithError(err).Error("couldn't build random events config form")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	opts.FormAction = "/championship/" + championshipID + "/random-events"

	ch.viewRenderer.MustLoadTemplate(w, r, "custom-race/new.html", opts)
}

// generateRandomEvents parses a race config from the form and creates N pending events,
// each picking a random pool-linked class and a random track from that class's pool.
// POST /championship/{championshipID}/random-events
func (ch *ChampionshipsHandler) generateRandomEvents(w http.ResponseWriter, r *http.Request) {
	championshipID := chi.URLParam(r, "championshipID")

	championship, err := ch.championshipManager.LoadChampionship(championshipID)
	if err != nil {
		logrus.WithError(err).Error("couldn't load championship for random events")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	raceConfig, err := ch.championshipManager.BuildCustomRaceFromForm(r)
	if err != nil {
		logrus.WithError(err).Error("couldn't parse race config for random events")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	numEvents := 1
	if n, parseErr := strconv.Atoi(r.FormValue("NumRandomEvents")); parseErr == nil && n > 0 {
		if n > 50 {
			n = 50
		}
		numEvents = n
	}

	// Build slice of pool-linked classes that have tracks available.
	type classWithPool struct {
		class *ChampionshipClass
		pool  *TrackCarPool
	}
	var eligible []classWithPool
	for i := range championship.Classes {
		class := championship.Classes[i]
		if class.PoolID == uuid.Nil {
			continue
		}
		pool, loadErr := ch.championshipManager.store.LoadTrackCarPool(class.PoolID.String())
		if loadErr != nil || len(pool.Tracks) == 0 {
			logrus.WithError(loadErr).Warnf("skipping class %s: pool has no tracks", class.Name)
			continue
		}
		eligible = append(eligible, classWithPool{class: class, pool: pool})
	}

	if len(eligible) == 0 {
		AddErrorFlash(w, r, "No pool-linked classes found. Add pools to the championship classes first.")
		http.Redirect(w, r, "/championship/"+championshipID, http.StatusFound)
		return
	}

	avoidRepeat := r.FormValue("AvoidRepeatTracks") == "1"
	usedTracks := make(map[uuid.UUID]map[string]bool) // poolID → "track|layout"

	// Generate events into per-class buckets for interleaving.
	// Round-robin through eligible classes so classes alternate rather than cluster.
	classBuckets := make(map[uuid.UUID][]*ChampionshipEvent)
	var classOrder []uuid.UUID
	for _, e := range eligible {
		if _, seen := classBuckets[e.class.ID]; !seen {
			classOrder = append(classOrder, e.class.ID)
			classBuckets[e.class.ID] = nil
		}
	}
	// Shuffle class order for variety each run.
	rand.Shuffle(len(classOrder), func(i, j int) { classOrder[i], classOrder[j] = classOrder[j], classOrder[i] })

	// Build a pool-lookup by class ID for the round-robin loop.
	poolByClassID := make(map[uuid.UUID]*TrackCarPool)
	for _, e := range eligible {
		poolByClassID[e.class.ID] = e.pool
	}

	for i := 0; i < numEvents; i++ {
		classID := classOrder[i%len(classOrder)]
		pool := poolByClassID[classID]

		tracks := pool.Tracks
		var pt PoolTrack
		if avoidRepeat {
			poolID := classID // use classID as bucket key (one pool per class)
			used := usedTracks[poolID]
			if used == nil {
				used = make(map[string]bool)
				usedTracks[poolID] = used
			}
			var unused []PoolTrack
			for _, t := range tracks {
				if !used[t.Track+"|"+t.Layout] {
					unused = append(unused, t)
				}
			}
			if len(unused) > 0 {
				tracks = unused
			}
			pt = tracks[rand.Intn(len(tracks))]
			usedTracks[poolID][pt.Track+"|"+pt.Layout] = true
		} else {
			pt = tracks[rand.Intn(len(tracks))]
		}

		event := NewChampionshipEvent()
		event.ClassID = classID
		event.RaceSetup = *raceConfig
		event.RaceSetup.Track = pt.Track
		event.RaceSetup.TrackLayout = resolveTrackLayout(pt.Track, pt.Layout)

		classBuckets[classID] = append(classBuckets[classID], event)
	}

	// Interleave events from buckets: GTP → DTM → GTP → DTM → ...
	created := 0
	for {
		added := false
		for _, classID := range classOrder {
			if len(classBuckets[classID]) > 0 {
				championship.Events = append(championship.Events, classBuckets[classID][0])
				classBuckets[classID] = classBuckets[classID][1:]
				created++
				added = true
			}
		}
		if !added {
			break
		}
	}

	if err := ch.championshipManager.UpsertChampionship(championship); err != nil {
		logrus.WithError(err).Error("couldn't save championship after generating random events")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	AddFlash(w, r, fmt.Sprintf("Generated %d random event(s) from pools!", created))
	http.Redirect(w, r, "/championship/"+championshipID, http.StatusFound)
}

// resolveTrackLayout returns layout if non-empty. Otherwise it inspects the track
// directory and returns the first valid sublayout (has data/surfaces.ini). This
// prevents events from being generated with an empty layout on multi-layout tracks,
// which would cause wrong spawn positions and missing map images.
func resolveTrackLayout(track, layout string) string {
	if layout != "" {
		return layout
	}
	tracksPath := filepath.Join(ServerInstallPath, "content", "tracks")
	trackDir := filepath.Join(tracksPath, track)

	// If the track has a root data/surfaces.ini it is a single-layout track — keep "".
	if _, err := os.Stat(filepath.Join(trackDir, "data", "surfaces.ini")); err == nil {
		return ""
	}

	// Multi-layout track: pick the first subdirectory with data/surfaces.ini.
	entries, err := ioutil.ReadDir(trackDir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		switch e.Name() {
		case "data", "ui":
			continue
		}
		if _, err := os.Stat(filepath.Join(trackDir, e.Name(), "data", "surfaces.ini")); err == nil {
			return e.Name()
		}
	}
	return ""
}
