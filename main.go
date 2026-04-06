package main

import (
	"context"
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

	notifChan := make(chan sptt.Notif, 10)

	port := "8080"
	if v, ok := env["API_PORT"]; ok && v != "" {
		port = v
	}

	corsOrigin := "*"
	if v, ok := env["CORS_ORIGIN"]; ok && v != "" {
		corsOrigin = v
	}

	apiServer := api.NewSpttAPI(ctx, db, notifChan, &wg, ":"+port, corsOrigin)

	// Run routines for stApi and monitor
	wg.Add(2)
	go monitor(ctx, db, stApi, notifChan, &wg)
	log.Info("Steam Playtime Tracker started.")

	go apiServer.Run()
	log.Info("API server started on port ", port)

	<-cancelChan
	cancel()
	log.Info("Shutting down Steam Playtime Tracker, waiting for routines to finish.")
	wg.Wait()
	close(notifChan)
	log.Info("Exiting...")
}

// Main process for monitoring games and
// playtime sessions for differen steam
// users
func monitor(ctx context.Context, db *sptt.DB, api *sptt.SteamAPI, notifChan chan sptt.Notif, wg *sync.WaitGroup) {
	defer wg.Done()
	wgMonitor := sync.WaitGroup{}

	ids, err := db.GetActiveSteamIDs(ctx)

	ticker := time.NewTicker(1 * time.Minute)

	// Handle external updates
	wgMonitor.Add(1)
	go func() {
		defer wgMonitor.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case notif := <-notifChan:
				if notif.IsUserListUpdate() {
					log.Info("Internal user list update request received.")
					ids, err = db.GetActiveSteamIDs(ctx)
					if err != nil {
						log.Error("Error while trying to get steam ids from db: ", err)
					}
				} else {
					log.Error("Unknown notification received on notifChan with message type: ", notif.MessageType, " and payload: ", notif.Payload)
				}
			}
		}
	}()

	wgMonitor.Add(1)
	go func() {
		defer wgMonitor.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				log.Info("Running user updates for", time.Now().UTC())
				summaries, err := api.GetPlayerSummaries(ctx, ids)
				if err != nil {
					log.Error("Error while trying to get player summaries: ", err)
					continue
				}
				for _, id := range ids {
					summary, ok := summaries[id]
					if !ok {
						log.Error("Summary for user ", id, " not found in summaries, skipping")
						continue
					}
					go updateUser(ctx, db, api, id, summary)
				}
			}
		}
	}()
	wgMonitor.Wait()
}

// Updates user sessions based on the user's current status
func updateUser(ctx context.Context, db *sptt.DB, api *sptt.SteamAPI, id sptt.SteamID, summary sptt.PlayerSummary) {
	if summary.SteamID != id {
		log.Error("SteamID mismatch, expected ", id, " got ", summary.SteamID)
		log.Error("Skipping user ", id, "/", summary.SteamID)
		return
	}

	if summary.Visibility != 3 {
		log.Debug("User ", id, " has a private profile")
		log.Info("Releasing active sessions for user ", id)
		err := db.RemoveActiveSessions(ctx, id)
		if err != nil {
			log.Error("Error while trying to remove active sessions for user ", id, ": ", err)
		}
		return
	}

	if summary.GameID == 0 {
		log.Debug("User ", id, " is not in game")
		activeSessions, err := db.GetActiveSessions(ctx, id)
		if err != nil {
			log.Error("Error while trying to get active sessions for user ", id, ": ", err)
			return
		}

		if len(activeSessions) == 0 {
			log.Debug("User ", id, " has no active sessions, nothing to update")
			return
		} else {
			log.Debug("User ", id, " has active sessions, releasing them")
			now := time.Now().UTC().Truncate(time.Second)

			appids := make([]sptt.AppID, 0, len(activeSessions))
			for _, sess := range activeSessions {
				appids = append(appids, sess.AppID)
			}

			games, err := api.GetOwnedGames(ctx, id, appids)
			if err != nil {
				log.Error("Error while trying to get owned games for user ", id, ": ", err)
				return
			}

			for _, sess := range activeSessions {
				if game, ok := games[sess.AppID]; ok {
					playtimeDiffSteam := game.Playtime - sess.PlaytimeForever
					playtimeDiffServer := uint32(now.Sub(sess.UTCStart).Abs().Minutes())

					if playtimeDiffSteam == 0 {
						log.Debug("No playtime difference for game ", sess.AppID, " for user ", id, ", deferring session to next minute")
					} else {
						newSession := sptt.Session{
							SteamID:         id,
							UTCStart:        sess.UTCStart,
							PlaytimeForever: game.Playtime,
							AppID:           sess.AppID,
						}
						// NOTE: This could be dangerous, we are making many assumptions here
						if playtimeDiffServer-playtimeDiffSteam > 3 { // 3 minutes difference max
							log.Warn("Significant playtime difference for ActiveSession ", sess, " for user ", id, ", durationSteam: ", playtimeDiffSteam, ", durationServer: ", playtimeDiffServer)
							log.Info("Using Steam playtime as reference")

							newSession.UTCEnd = sess.UTCStart.Add(time.Duration(playtimeDiffSteam) * time.Minute)
						} else {
							newSession.UTCEnd = now
						}

						err = db.AddSession(ctx, newSession)
						if err != nil {
							log.Error("Error while trying to add session for user ", id, ": ", err, "; session will be released anyways")
						}

						err = db.RemoveActiveSession(ctx, id, sess.AppID)
						if err != nil {
							log.Error("Error while trying to remove active session for user ", id, ": ", err)
							return
						}

						log.Info("Released session for user ", id, " in game ", sess.AppID)
					}
				} else {
					log.Error("Game ", sess.AppID, " not found in owned games for user ", id, ", releasing session")
					err = db.RemoveActiveSession(ctx, id, sess.AppID)
					if err != nil {

					}
				}
			}
		}
	} else {
		log.Debug("User ", id, " is in game ", summary.GameID)
		activeSessions, err := db.GetActiveSessions(ctx, id)
		if err != nil {
			log.Error("Error while trying to get active sessions for user ", id, ": ", err)
			return
		}

		sessThisGame, alreadyPlaying := activeSessions[summary.GameID]

		if len(activeSessions) == 0 || !alreadyPlaying {
			var playtime uint32 = 0
			game, err := api.GetOwnedGame(ctx, id, summary.GameID)
			if err != nil {
				if err == sptt.ErrEmptyGames {
					log.Error("Can't get summary for game from GetOwnedGame, PlaytimeForever will be 0")
				} else {
					log.Error("Error while trying to get owned game for user ", id, ": ", err)
					return
				}
			} else {
				playtime = game.Playtime
			}

			// TODO: Add game cache here, game name, etc. iff that game is not in cache

			log.Debug("User ", id, " has no active sessions, starting new session")
			sess := sptt.ActiveSession{
				SteamID:         id,
				UTCStart:        time.Now().UTC().Truncate(time.Second),
				PlaytimeForever: playtime,
				AppID:           summary.GameID,
			}

			err = db.AddActiveSession(ctx, sess)
			if err != nil {
				log.Error("Error while trying to add active session for user ", id, ": ", err)
				return
			}
			log.Info("Started new session for user ", id, " in game ", summary.GameID)
		} else {
			log.Debug("User ", id, " is already playing game ", summary.GameID, " since ", sessThisGame.UTCStart)
		}
	}
}
