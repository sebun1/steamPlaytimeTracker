package main

import (
	"context"
	"os"

	"github.com/sebun1/steamPlaytimeTracker/log"
	"github.com/sebun1/steamPlaytimeTracker/sptt"
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
	ctx := context.Background()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	api := sptt.NewSteamAPI(env["STEAM_API_KEY"])

	db, err := sptt.NewDB(env["DB_USER"], env["DB_PASSWORD"], env["DB_NAME"])
	if err != nil {
		log.Fatal(err)
		return
	}
	defer db.Close()

	// Test API key
	err = api.TestAPIKey(ctx)
	if err != nil {
		if err == sptt.ErrForbidden {
			log.Fatal("API key is invalid, steam responded with 403 Forbidden.")
			return
		}
		log.Fatal(err)
		log.Fatal("API key validation failed.")
		return
	}

	ids, err := db.GetSteamIDs(ctx)
	if err != nil {
		log.Error(err)
		return
	}

	summaries, err := api.GetPlayerSummaries(ctx, ids)
	if err != nil {
		log.Fatal(err)
		return
	}
	log.Debug(summaries[ids[0]].Personaname)

	games, err := api.GetOwnedGames(ctx, ids[0], []string{"493520"})
	if err != nil {
		log.Fatal(err)
		return
	}
	log.Debug(games["493520"].Name, games["493520"].Playtime)
}

func track(ctx context.Context, db *sptt.DB, api *sptt.SteamAPI) {
	//TODO: When APIs return errors, they should be handled gracefully, DO NOT PANIC
	ids, err := db.GetSteamIDs(ctx)
	if err != nil {
		log.Error(err)
		return
	}

	for _, id := range ids {
		log.Debug("Handling player ", id)

	}
}
