package main

import (
	"log"

	"github.com/sebun1/steamPlaytimeTracker/sptt"

	"fmt"
)

func main() {
	env := sptt.GetEnv(".env")
	api := sptt.SteamAPI{APIKey: env["STEAM_API_KEY"]}
	if api.APIKey == "" {
		log.Fatal("STEAM_API_KEY is not set.")
		return
	}
	steamids := "76561198854733565"
	resp := api.GetPlayerSummaries(steamids)
	fmt.Printf("%v\n", resp)
}
