package sptt

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"

	"github.com/sebun1/steamPlaytimeTracker/log"

	_ "github.com/lib/pq"
)

const (
	ErrSQLFileNotFound = DBError("SQL file (db.sql) not found")
)

type DBError string

func (e DBError) Error() string {
	return string(e)
}

type DB struct {
	db *sql.DB
}

// This is for testing purposes only
func newDBWithSQLFile(user, pwd, dbname, sqlfile string) (*DB, error) {
	//ssl_mode := "verify-full"
	ssl_mode := "disable"

	log.Info("Connecting to database...")
	db, err := sql.Open("postgres", fmt.Sprintf("user=%s password=%s dbname=%s sslmode=%s", user, pwd, dbname, ssl_mode))
	if err != nil {
		return nil, err
	}

	thisdb := &DB{db}

	log.Info("Initializing database...")
	err = thisdb.init(sqlfile)
	if err != nil {
		return nil, err
	}

	return thisdb, nil
}

func NewDB(user, pwd, dbname string) (*DB, error) {
	//ssl_mode := "verify-full"
	ssl_mode := "disable"

	log.Info("Connecting to database...")
	db, err := sql.Open("postgres", fmt.Sprintf("user=%s password=%s dbname=%s sslmode=%s", user, pwd, dbname, ssl_mode))
	if err != nil {
		return nil, err
	}

	thisdb := &DB{db}

	log.Info("Initializing database...")
	err = thisdb.init("db.sql")
	if err != nil {
		return nil, err
	}

	return thisdb, nil
}

func (d *DB) Close() {
	d.db.Close()
}

func (d *DB) init(filename string) error {
	var query []byte
	query, err := os.ReadFile(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return ErrSQLFileNotFound
		}
		return err
	}

	log.Debug("Create DB Query:\n", string(query))

	_, err = d.db.Exec(string(query))
	if err != nil {
		return err
	}

	return nil
}

func (d *DB) GetSteamIDs(ctx context.Context) ([]SteamID, error) {
	rows, err := d.db.QueryContext(ctx, "SELECT steamid FROM users")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	steamids := []SteamID{}
	for rows.Next() {
		var steamid SteamID
		err := rows.Scan(&steamid)
		if err != nil {
			return nil, err
		}
		steamids = append(steamids, steamid)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return steamids, nil
}

func (d *DB) AddSteamID(ctx context.Context, ids []SteamID) error {
	stmt, err := d.db.PrepareContext(ctx, "INSERT INTO users(steamid, enabled, public) VALUES($1, true, true) ON CONFLICT (steamid) DO NOTHING")
	if err != nil {
		return wrapErr(err)
	}
	defer stmt.Close()

	for _, id := range ids {
		res, err := stmt.ExecContext(ctx, id)
		if err != nil {
			return wrapErr(err)
		}

		affected, err := res.RowsAffected()
		if err != nil {
			log.Error("Error getting rows affected: ", err)
			continue
		}

		if affected == 0 {
			log.Warn("SteamID", id, "already exist in the database")
		}
	}
	return nil
}

func (d *DB) RemoveSteamID(ctx context.Context, steamid []SteamID) error {
	stmt, err := d.db.PrepareContext(ctx, "DELETE FROM users WHERE steamid = $1")
	if err != nil {
		return wrapErr(err)
	}
	defer stmt.Close()

	for _, id := range steamid {
		res, err := stmt.Exec(id)
		if err != nil {
			return wrapErr(err)
		}

		affected, err := res.RowsAffected()
		if err != nil {
			log.Error("Error getting rows affected: ", err)
			continue
		}

		if affected == 0 {
			log.Warn("SteamID not found: ", id)
		}
	}
	return nil
}

type ActiveSession struct {
	SteamID         SteamID
	UTCStart        time.Time
	PlaytimeForever uint32
	AppID           AppID
}

// GetActiveSessions returns all active sessions for a steamid
// This is used to check if a user is already active in a game
func (d *DB) GetActiveSessions(ctx context.Context, id SteamID) (map[AppID]ActiveSession, error) {
	rows, err := d.db.QueryContext(ctx, "SELECT steamid, utcstart, playtime_forever, appid FROM active_sessions WHERE steamid = $1", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	sessionsMap := make(map[AppID]ActiveSession)
	for rows.Next() {
		var session ActiveSession
		err := rows.Scan(&session.SteamID, &session.UTCStart, &session.PlaytimeForever, &session.AppID)
		if err != nil {
			return nil, err
		}
		sessionsMap[session.AppID] = session
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return sessionsMap, nil
}

// Adds an active session to the database
// This is better for when we handle users in
// parallel; each user can only have one session
// added at a time
func (d *DB) AddActiveSession(ctx context.Context, session ActiveSession) error {
	stmt, err := d.db.PrepareContext(ctx, "INSERT INTO active_sessions(steamid, utcstart, playtime_forever, appid) VALUES($1, $2, $3, $4)")
	if err != nil {
		return wrapErr(err)
	}
	defer stmt.Close()

	_, err = stmt.ExecContext(ctx, session.SteamID, session.UTCStart, session.PlaytimeForever, session.AppID)
	if err != nil {
		return wrapErr(err)
	}

	return nil
}

// Adds multiple active sessions to database
// Prefer AddActiveSession for single session
// Overhead of PrepareContext might be negligible
// even for multiple sessions
func (d *DB) AddActiveSessions(ctx context.Context, sessions []ActiveSession) error {
	stmt, err := d.db.PrepareContext(ctx, "INSERT INTO active_sessions(steamid, utcstart, playtime_forever, appid) VALUES($1, $2, $3, $4)")
	if err != nil {
		return wrapErr(err)
	}
	defer stmt.Close()

	for _, session := range sessions {
		_, err = stmt.ExecContext(ctx, session.SteamID, session.UTCStart, session.PlaytimeForever, session.AppID)
		if err != nil {
			return wrapErr(err)
		}
	}

	return nil
}

// Removes an active session from the database
// This should only be used to cancel certain
// sessions
//
// Currently unused. User cannot cancel sessions,
// everything is automatic
func (d *DB) RemoveActiveSession(ctx context.Context, steamid SteamID, utcstart time.Time) error {
	stmt, err := d.db.PrepareContext(ctx, "DELETE FROM active_sessions WHERE steamid = $1 AND utcstart = $2")
	if err != nil {
		return wrapErr(err)
	}
	defer stmt.Close()

	res, err := stmt.ExecContext(ctx, steamid, utcstart)
	if err != nil {
		return wrapErr(err)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return wrapErr(err)
	}

	if affected == 0 {
		log.Warn("Active session not found: ", steamid, utcstart)
	}

	return nil
}

// Removes all active sessions for a steamid
// This should be preferred since we only remove
// active sessions when the user is no longer active
// in any of the games
func (d *DB) RemoveActiveSessions(ctx context.Context, steamid SteamID) error {
	stmt, err := d.db.PrepareContext(ctx, "DELETE FROM active_sessions WHERE steamid = $1")
	if err != nil {
		return wrapErr(err)
	}
	defer stmt.Close()

	res, err := stmt.ExecContext(ctx, steamid)
	if err != nil {
		return wrapErr(err)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return wrapErr(err)
	}

	if affected == 0 {
		log.Warn("Active session not found: ", steamid)
	}

	return nil
}

type Session struct {
	SteamID         SteamID
	UTCStart        time.Time
	UTCEnd          time.Time
	PlaytimeForever uint32
	AppID           AppID
}

func (d *DB) GetSessions(ctx context.Context, id SteamID) ([]Session, error) {
	rows, err := d.db.QueryContext(ctx, "SELECT steamid, utcstart, utcend, playtime_forever, appid FROM sessions WHERE steamid = $1 ORDER BY utcstart ASC", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	sessions := []Session{}
	for rows.Next() {
		var session Session
		err := rows.Scan(&session.SteamID, &session.UTCStart, &session.UTCEnd, &session.PlaytimeForever, &session.AppID)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return sessions, nil
}

func (d *DB) AddSession(ctx context.Context, session Session) error {
	stmt, err := d.db.PrepareContext(ctx, "INSERT INTO sessions(steamid, utcstart, utcend, playtime_forever, appid) VALUES($1, $2, $3, $4, $5)")
	if err != nil {
		return wrapErr(err)
	}
	defer stmt.Close()

	_, err = stmt.ExecContext(ctx, session.SteamID, session.UTCStart, session.UTCEnd, session.PlaytimeForever, session.AppID)
	if err != nil {
		return wrapErr(err)
	}

	return nil
}

type GameCache struct {
	AppID           AppID
	Name            string
	Publisher       string
	Developer       string
	HeaderImage     string
	Recommendations uint32
}

func (d *DB) GetGameCache(ctx context.Context, appid AppID) (GameCache, error) {
	var game GameCache
	err := d.db.QueryRowContext(ctx, "SELECT appid, name, publisher, developer, header_image, recommendations FROM game_cache WHERE appid = $1", appid).Scan(&game.AppID, &game.Name, &game.Publisher, &game.Developer, &game.HeaderImage, &game.Recommendations)
	if err != nil {
		return game, err
	}
	return game, nil
}

func (d *DB) AddGameCache(ctx context.Context, game GameCache) error {
	stmt, err := d.db.PrepareContext(ctx, "INSERT INTO game_cache(appid, name, publisher, developer, header_image, recommendations) VALUES($1, $2, $3, $4, $5, $6)")
	if err != nil {
		return wrapErr(err)
	}
	defer stmt.Close()

	_, err = stmt.ExecContext(ctx, game.AppID, game.Name, game.Publisher, game.Developer, game.HeaderImage, game.Recommendations)
	if err != nil {
		return wrapErr(err)
	}

	return nil
}
