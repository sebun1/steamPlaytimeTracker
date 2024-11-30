package sptt

import (
	"context"
	"testing"
)

func TestSteamAPI(t *testing.T) {
	ctx := context.Background()
	env, err := GetEnv("../.env")
	if err != nil {
		t.Errorf("Expected nil, got %v", err)
	}
	api := NewSteamAPI(env["STEAM_API_KEY"])
	ids := []SteamID{76561198854733565}

	t.Run("GetPlayerSummaries", func(t *testing.T) {
		summaries, err := api.GetPlayerSummaries(ctx, ids)
		if err != nil {
			t.Errorf("Expected nil, got %v", err)
			return
		}
		_, ok := summaries[ids[0]]

		if !ok || summaries[ids[0]].SteamID != ids[0] {
			t.Errorf("Expected summary to be valid, got invalid entry")
		}
	})

	t.Run("GetOwnedGames", func(t *testing.T) {
		games, err := api.GetOwnedGames(ctx, ids[0], []AppID{493520})
		if err != nil {
			t.Errorf("Expected nil, got %v", err)
			return
		}

		if games[493520].AppID != 493520 || games[493520].Name != "GTFO" {
			t.Errorf("Expected game to be valid, got invalid entry")
		}
	})
}
