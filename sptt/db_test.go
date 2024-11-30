package sptt

import (
	"context"
	"testing"
	"time"
)

func TestSteamID(t *testing.T) {
	env, err := GetEnv("../.env")
	if err != nil {
		t.Errorf("Expected nil, got %v", err)
	}
	if _, ok := env["DB_USER"]; !ok {
		t.Errorf("Expected DB_USER, got nil")
	}
	if _, ok := env["DB_PASSWORD"]; !ok {
		t.Errorf("Expected DB_PASSWORD, got nil")
	}
	if _, ok := env["DB_NAME"]; !ok {
		t.Errorf("Expected DB_NAME, got nil")
	}

	ctx := context.Background()

	db, err := newDBWithSQLFile(env["DB_USER"], env["DB_PASSWORD"], env["DB_NAME"], "../db.sql")
	if err != nil {
		t.Errorf("Expected nil, got %v", err)
	}

	t.Run("Retrieve SteamID", func(t *testing.T) {
		_, err := db.GetSteamIDs(ctx)
		if err != nil {
			t.Errorf("Expected nil, got %v", err)
		}
	})

	t.Run("Insert SteamID", func(t *testing.T) {
		err := db.AddSteamID(ctx, SteamID(76561198000000000), "Test")
		if err != nil {
			t.Errorf("Expected nil, got %v", err)
		}
	})

	t.Run("Remove SteamID", func(t *testing.T) {
		err := db.RemoveSteamID(ctx, []SteamID{76561198000000000})
		if err != nil {
			t.Errorf("Expected nil, got %v", err)
		}
	})

	t.Run("Remove non-existent SteamID", func(t *testing.T) {
		err := db.RemoveSteamID(ctx, []SteamID{76561198000000000})
		if err != nil {
			t.Errorf("Expected nil, got %v", err)
		}
	})
}

func TestSessions(t *testing.T) {
	env, err := GetEnv("../.env")
	if err != nil {
		t.Errorf("Expected nil, got %v", err)
	}
	db, err := newDBWithSQLFile(env["DB_USER"], env["DB_PASSWORD"], env["DB_NAME"], "../db.sql")
	ctx := context.Background()

	tm := time.Date(2024, time.November, 28, 12, 0, 0, 0, time.Local)
	pt := uint32(128)
	appid := AppID(493520)
	steamid := SteamID(76561198000000000)

	t.Run("Add Active Session", func(t *testing.T) {

		err := db.AddActiveSession(ctx, ActiveSession{
			SteamID:         steamid,
			UTCStart:        tm,
			PlaytimeForever: uint32(pt),
			AppID:           appid,
		})
		if err != nil {
			t.Errorf("Expected nil, got %v", err)
		}
	})

	t.Run("Retrieve Active Sessions", func(t *testing.T) {
		sess, err := db.GetActiveSessions(ctx, 76561198000000000)
		if err != nil {
			t.Errorf("Expected nil, got %v", err)
		}

		if len(sess) == 0 {
			t.Errorf("Expected non-empty session list, got empty")
		}

		if got := sess[appid].PlaytimeForever; got != pt {
			t.Errorf("Expected %d, got %d", pt, got)
		}

		if got := sess[appid].UTCStart.Truncate(time.Second); got.Equal(tm.Truncate(time.Second)) {
			t.Errorf("Expected %v, got %v", tm, got)
		}

		if got := sess[appid].SteamID; got != steamid {
			t.Errorf("Expected %d, got %d", steamid, got)
		}
	})

	t.Run("Remove Active Session", func(t *testing.T) {
		err := db.RemoveActiveSession(ctx, steamid, appid)
		if err != nil {
			t.Errorf("Expected nil, got %v", err)
		}
	})

}
