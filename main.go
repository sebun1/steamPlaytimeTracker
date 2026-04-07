package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/sebun1/steamPlaytimeTracker/log"
	"github.com/sebun1/steamPlaytimeTracker/sptt"
	"github.com/sebun1/steamPlaytimeTracker/sptt/api"
)

var env map[string]string

func init() {
	hadError := false

	var err error
	env, err = sptt.GetEnv(".env")
	if err != nil {
		log.Fatal(err)
		os.Exit(1)
	}

	if v, ok := env["LOG_LEVEL"]; ok || v != "" {
		log.SetLevelFromString(v)
	}

	varChecks := []string{
		"STEAM_API_KEY",
		"DB_USER",
		"DB_PASSWORD",
		"DB_NAME",
	}

	for _, v := range varChecks {
		if _, ok := env[v]; !ok {
			log.Fatal(v + " is not set.")
			hadError = true
		}
	}

	if hadError {
		log.Fatal("Exiting due to errors.")
		os.Exit(1)
	}
}

type Application struct {
	DB        *sptt.DB
	SteamAPI  *sptt.SteamAPI
	NotifChan chan sptt.Notif
	UserIDs   []sptt.SteamID
}

func main() {
	wg := sync.WaitGroup{}

	ctx := context.Background()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	cancelChan := make(chan os.Signal, 1)
	signal.Notify(cancelChan, os.Interrupt, syscall.SIGTERM)

	stApi := sptt.NewSteamAPI(env["STEAM_API_KEY"])

	db, err := sptt.NewDB(env["DB_USER"], env["DB_PASSWORD"], env["DB_NAME"])
	if err != nil {
		log.Fatal(err)
		return
	}

	defer func(db *sptt.DB) {
		err := db.Close()
		if err != nil {
			log.Errorf("Error while closing database connection: %e", err)
		}
	}(db)

	// Test API key
	err = stApi.TestAPIKey(ctx)
	if err != nil {
		if err == sptt.ErrForbidden {
			log.Fatal("API key is invalid, steam responded with 403 Forbidden.")
			return
		}
		log.Fatal(err)
		log.Fatal("API key validation failed.")
		return
	}

	// Test DB connection
	err = db.Ping(ctx)
	if err != nil {
		log.Fatal(err)
		return
	}

	// monitor <-> API server communication channel.
	notifChan := make(chan sptt.Notif, 10)

	port := "8080"
	if v, ok := env["API_PORT"]; ok && v != "" {
		port = v
	}

	corsOrigin := "*"
	if v, ok := env["CORS_ORIGIN"]; ok && v != "" {
		corsOrigin = v
	}

	apiServer := api.NewSptAPI(ctx, db, notifChan, &wg, ":"+port, corsOrigin)

	ids, err := db.GetActiveSteamIDs(ctx)
	if err != nil {
		log.Fatal("Error while trying to get active steam ids from db: ", err)
		return
	}

	app := &Application{
		DB:        db,
		SteamAPI:  stApi,
		NotifChan: notifChan,
		UserIDs:   ids,
	}

	// Run routines for stApi and monitor
	wg.Add(1)
	go app.monitor(ctx, &wg)
	log.Info("Steam Playtime Tracker started.")

	wg.Add(1)
	go apiServer.Run()
	log.Info("API server started on port ", port)

	// Wait for shutdown signal
	<-cancelChan
	cancel()
	log.Info("Shutting down Steam Playtime Tracker, waiting for routines to finish.")
	wg.Wait()
	close(notifChan)
	log.Info("Exiting...")
}

// Main process for monitoring games and
// playtime sessions for different steam
// users
func (app *Application) monitor(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	monitorWg := &sync.WaitGroup{}

	monitorWg.Add(2)
	go monitorSignalHandler(ctx, app, monitorWg)
	go monitorLoop(ctx, app, monitorWg)

	monitorWg.Wait()
}

// Handles notifications for the monitor
func monitorSignalHandler(ctx context.Context, app *Application, wg *sync.WaitGroup) {
	defer wg.Done()

	for {
		select {
		case <-ctx.Done():
			return

		case notif := <-app.NotifChan:
			if notif.IsUserListUpdate() {
				log.Info("Processing user list update...")
				ids, err := app.DB.GetActiveSteamIDs(ctx)
				if err != nil {
					log.Error("Error while trying to get steam ids from db: ", err)
					continue
				}
				app.UserIDs = ids
				continue
			}

			log.Errorf("monitor: unknown notification received, type: %v payload: %v", notif.MessageType, notif.Payload)
		}
	}
}

// Main loop for minute-wise updates of user sessions
func monitorLoop(ctx context.Context, app *Application, wg *sync.WaitGroup) {
	defer wg.Done()

	ticker := time.NewTicker(1 * time.Minute)

	for {
		select {
		case <-ctx.Done():
			ticker.Stop()
			return
		case <-ticker.C:
			log.Debug("Running user updates for", time.Now().UTC())
			summaries, err := app.SteamAPI.GetPlayerSummaries(ctx, app.UserIDs)
			if err != nil {
				log.Error("Error while trying to get player summaries: ", err)
				continue
			}
			for _, id := range app.UserIDs {
				summary, ok := summaries[id]
				if !ok {
					log.Error("Summary for user ", id, " not found in summaries, skipping")
					continue
				}
				go app.processUser(ctx, id, summary)
			}
		}
	}
}

// Processes a user update
func (app *Application) processUser(ctx context.Context, id sptt.SteamID, summary sptt.PlayerSummary) {
	if summary.SteamID != id {
		log.Error("SteamID mismatch, expected ", id, " got ", summary.SteamID)
		log.Error("Skipping user ", id, "/", summary.SteamID)
		return
	}

	if summary.Visibility != 3 {
		log.Debugf("User %v has a private profile", id)
		log.Infof("Releasing active_sessions for %v b/c private profile", id)
		err := app.DB.RemoveActiveSessions(ctx, id)
		if err != nil {
			log.Errorf("Error removing active_sessions for %v: %v", id, err)
		}
		return
	}

	// User in game
	if summary.GameID != nil {
		err := app.startSession(ctx, id, summary)
		if err != nil {
			log.Error("Failed to start session for %v", id)
		}
		return
	}

	// User not in game
	log.Debugf("User %v is not in game", id)
	activeSessions, err := app.DB.GetActiveSessions(ctx, id)
	if err != nil {
		log.Errorf("Error getting active_sessions for %v: %v", id, err)
		return
	}

	if len(activeSessions) > 0 {
		err := app.concludeSessions(ctx, id, activeSessions)
		if err != nil {
			log.Errorf("Failed to conclude sessions for %v", id)
		}
	}
}

func (app *Application) startSession(ctx context.Context, id sptt.SteamID, summary sptt.PlayerSummary) error {
	if summary.GameID == nil {
		return fmt.Errorf("GameID is nil for %v, cannot start session", id)
	}

	gameId := *summary.GameID

	activeSessions, err := app.DB.GetActiveSessions(ctx, id)
	if err != nil {
		log.Errorf("Error while trying to get active sessions for user %v: %v", id, err)
		return err
	}

	existingSession, alreadyPlaying := activeSessions[gameId]

	// if gameId is already in active sessions, do nothing
	if alreadyPlaying {
		log.Debugf("User %v is already playing game %v since %v", id, gameId, existingSession.UTCStart)
		return nil
	}

	var playtime int32 = 0
	game, err := app.SteamAPI.GetOwnedGame(ctx, id, gameId)
	if err != nil && err != sptt.ErrEmptyGames {
		log.Errorf("Error while trying to get owned game for user %v: %v", id, err)
		return err
	}

	if err == sptt.ErrEmptyGames || game.Playtime2Weeks == nil {
		log.Warnf("Games or playtime for user %v are empty/private, PlaytimeForever will be -1", id)
		playtime = -1
	} else {
		playtime = game.Playtime
	}

	log.Debug("User %v has no active sessions, starting new session", id)
	sess := sptt.ActiveSession{
		SteamID:         id,
		UTCStart:        time.Now().UTC().Truncate(time.Second),
		PlaytimeForever: playtime,
		AppID:           *summary.GameID,
	}

	err = app.DB.AddActiveSession(ctx, sess)
	if err != nil {
		log.Errorf("Error adding active session for %v: %v", id, err)
		return err
	}
	log.Infof("Started new session for %v in game %v", id, summary.GameID)

	return nil
}

func (app *Application) concludeSessions(ctx context.Context, id sptt.SteamID, activeSessions map[sptt.AppID]sptt.ActiveSession) error {
	log.Debug("User ", id, " has active sessions, releasing them")
	now := time.Now().UTC().Truncate(time.Second)

	appids := make([]sptt.AppID, 0, len(activeSessions))
	for _, sess := range activeSessions {
		appids = append(appids, sess.AppID)
	}

	games, err := app.SteamAPI.GetOwnedGames(ctx, id, appids)

	// Case 3: Steam returned an empty response envelope - data is unavailable, defer to next cycle
	if err == sptt.ErrEmptyResponse {
		log.Warnf("GetOwnedGames returned empty response for user %v, deferring session conclusion", id)
		return nil
	}

	if err != nil && err != sptt.ErrEmptyGames {
		return fmt.Errorf("error getting owned games for user %v: %v", id, err)
	}

	// Case 1 (partial): ErrEmptyGames means the library is private or empty - no playtime data available
	playtimeAvailable := err == nil
	if !playtimeAvailable {
		log.Warnf("Games for user %v are empty/private, concluding sessions without playtime_forever", id)
	}

	for _, sess := range activeSessions {
		newSession := sptt.Session{
			SteamID:  id,
			UTCStart: sess.UTCStart,
			AppID:    sess.AppID,
		}

		game, gameFound := games[sess.AppID]

		if playtimeAvailable && gameFound && sess.PlaytimeForever != -1 {
			// Case 2: playtime_forever is available from Steam and we have a baseline from session start
			playtimeDiffSteam := game.Playtime - sess.PlaytimeForever
			playtimeDiffServer := int32(now.Sub(sess.UTCStart).Abs().Minutes())

			if playtimeDiffSteam == 0 {
				log.Debugf("No playtime difference for game %v for user %v, deferring to next minute", sess.AppID, id)
				continue
			}

			newSession.PlaytimeForever = game.Playtime
			if playtimeDiffServer-playtimeDiffSteam > 3 {
				log.Warnf("Significant playtime difference for game %v user %v: steam=%d server=%d minutes, using Steam's value", sess.AppID, id, playtimeDiffSteam, playtimeDiffServer)
				newSession.UTCEnd = sess.UTCStart.Add(time.Duration(playtimeDiffSteam) * time.Minute)
			} else {
				newSession.UTCEnd = now
			}
		} else {
			// Case 1: playtime_forever unavailable — library private/empty, game missing from response,
			// or session started without a playtime baseline (sess.PlaytimeForever == -1)
			if playtimeAvailable && !gameFound {
				log.Errorf("Game %v not found in owned games for user %v, concluding without playtime_forever", sess.AppID, id)
			} else if sess.PlaytimeForever == -1 {
				log.Warnf("Session for game %v user %v had no playtime baseline, concluding with server time only", sess.AppID, id)
			}
			newSession.PlaytimeForever = -1
			newSession.UTCEnd = now
		}

		if err := app.DB.AddSession(ctx, newSession); err != nil {
			log.Errorf("Error adding session for user %v game %v: %v; session will be released anyways", id, sess.AppID, err)
		}

		if err := app.DB.RemoveActiveSession(ctx, id, sess.AppID); err != nil {
			return fmt.Errorf("error removing active_session for user %v: %v", id, err)
		}

		log.Infof("Released session for user %v in game %v", id, sess.AppID)
	}
	return nil
}
