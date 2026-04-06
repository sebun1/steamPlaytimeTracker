package sptt

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/lib/pq"
	_ "github.com/lib/pq"
	"github.com/sebun1/steamPlaytimeTracker/log"
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

// For testing purposes only.
// Create db instance with a custom sql file.
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

// Creates a new database instance.
// Expects a steamtrack schema.
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

// Pings underlying database instance
func (d *DB) Ping(ctx context.Context) error {
	return d.db.PingContext(ctx)
}

// Closes underlying database instance
func (d *DB) Close() error {
	return d.db.Close()
}

// Initializes database with schema.
// Reads from `db.sql` file.
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

// Queries steam ID of all registered users
func (d *DB) GetSteamIDs(ctx context.Context) ([]SteamID, error) {
	rows, err := d.db.QueryContext(ctx, "SELECT steamid FROM users")
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	var steamids []SteamID
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

// Add steam ID to database with minimal information
func (d *DB) AddSteamID(ctx context.Context, id SteamID, uname string) error {
	stmt, err := d.db.PrepareContext(ctx, "INSERT INTO users(steamid, username, active, public) VALUES($1, $2, true, true) ON CONFLICT (steamid) DO NOTHING")
	if err != nil {
		return wrapErr(err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	res, err := stmt.ExecContext(ctx, id, uname)
	if err != nil {
		return wrapErr(err)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		log.Error("Error getting rows affected: ", err)
	}

	if affected == 0 {
		log.Warn("SteamID", id, "already exist in the database")
	}
	return nil
}

func (d *DB) RemoveSteamID(ctx context.Context, steamid []SteamID) error {
	stmt, err := d.db.PrepareContext(ctx, "DELETE FROM users WHERE steamid = $1")
	if err != nil {
		return wrapErr(err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()

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
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()

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

// AddActiveSession
//
// Adds an active session to the database
func (d *DB) AddActiveSession(ctx context.Context, session ActiveSession) error {
	return d.AddActiveSessions(ctx, []ActiveSession{session})
}

// AddActiveSessions
//
// Adds multiple active sessions to database
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

// RemoveActiveSession
//
// Removes an active session from the database
// This should only be used to cancel certain
// sessions
//
// Currently unused. User cannot cancel sessions,
// everything is automatic
func (d *DB) RemoveActiveSession(ctx context.Context, steamid SteamID, appid AppID) error {
	stmt, err := d.db.PrepareContext(ctx, "DELETE FROM active_sessions WHERE steamid = $1 AND appid = $2")
	if err != nil {
		return wrapErr(err)
	}
	defer stmt.Close()

	res, err := stmt.ExecContext(ctx, steamid, appid)
	if err != nil {
		return wrapErr(err)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return wrapErr(err)
	}

	if affected == 0 {
		log.Warn("Active session not found: ", steamid, appid)
	}

	return nil
}

// RemoveActiveSessions
//
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

// SessionSortBy is a whitelisted set of columns sessions can be sorted by.
type SessionSortBy string

const (
	SortByAppID           SessionSortBy = "appid"
	SortByUTCStart        SessionSortBy = "utcstart"
	SortByUTCEnd          SessionSortBy = "utcend"
	SortByPlaytimeForever SessionSortBy = "playtime_forever"
)

// SortDir is either ascending or descending.
type SortDir string

const (
	SortDirAsc  SortDir = "ASC"
	SortDirDesc SortDir = "DESC"
)

// SessionFilter holds optional filter conditions for session queries.
type SessionFilter struct {
	AppID              *AppID
	UTCStartFrom       *time.Time
	UTCStartTo         *time.Time
	UTCEndFrom         *time.Time
	UTCEndTo           *time.Time
	PlaytimeForeverMin *uint32
	PlaytimeForeverMax *uint32
}

// SessionQuery bundles pagination, sort, and filter options for GetSessions /
// GetSessionCount.
type SessionQuery struct {
	Page     int32
	PageSize int32
	SortBy   SessionSortBy
	SortDir  SortDir
	Filter   SessionFilter
}

// sessionWhereArgs builds the WHERE clause (excluding the fixed steamid=$1
// condition) and the corresponding args slice. argStart is the next $N index.
// Returns (clause, args, nextArgIdx).
func sessionWhereArgs(f SessionFilter, argStart int) (string, []interface{}, int) {
	var conds []string
	var args []interface{}
	i := argStart

	if f.AppID != nil {
		conds = append(conds, fmt.Sprintf("appid = $%d", i))
		args = append(args, *f.AppID)
		i++
	}
	if f.UTCStartFrom != nil {
		conds = append(conds, fmt.Sprintf("utcstart >= $%d", i))
		args = append(args, *f.UTCStartFrom)
		i++
	}
	if f.UTCStartTo != nil {
		conds = append(conds, fmt.Sprintf("utcstart <= $%d", i))
		args = append(args, *f.UTCStartTo)
		i++
	}
	if f.UTCEndFrom != nil {
		conds = append(conds, fmt.Sprintf("utcend >= $%d", i))
		args = append(args, *f.UTCEndFrom)
		i++
	}
	if f.UTCEndTo != nil {
		conds = append(conds, fmt.Sprintf("utcend <= $%d", i))
		args = append(args, *f.UTCEndTo)
		i++
	}
	if f.PlaytimeForeverMin != nil {
		conds = append(conds, fmt.Sprintf("playtime_forever >= $%d", i))
		args = append(args, *f.PlaytimeForeverMin)
		i++
	}
	if f.PlaytimeForeverMax != nil {
		conds = append(conds, fmt.Sprintf("playtime_forever <= $%d", i))
		args = append(args, *f.PlaytimeForeverMax)
		i++
	}

	clause := ""
	if len(conds) > 0 {
		clause = " AND " + strings.Join(conds, " AND ")
	}
	return clause, args, i
}

// safeSessionSortCol maps a SessionSortBy to its SQL column name.
// Defaults to "utcstart" for unknown values.
func safeSessionSortCol(s SessionSortBy) string {
	switch s {
	case SortByAppID:
		return "appid"
	case SortByUTCEnd:
		return "utcend"
	case SortByPlaytimeForever:
		return "playtime_forever"
	default:
		return "utcstart"
	}
}

// safeSortDir maps a SortDir to "ASC" or "DESC", defaulting to "ASC".
func safeSortDir(d SortDir) string {
	if d == SortDirDesc {
		return "DESC"
	}
	return "ASC"
}

// GetSessions returns paginated, optionally filtered and sorted concluded
// sessions for a steamid.
func (d *DB) GetSessions(ctx context.Context, id SteamID, q SessionQuery) ([]Session, error) {
	filterClause, filterArgs, nextIdx := sessionWhereArgs(q.Filter, 2)

	query := fmt.Sprintf(
		"SELECT steamid, utcstart, utcend, playtime_forever, appid FROM sessions WHERE steamid = $1%s ORDER BY %s %s LIMIT $%d OFFSET $%d",
		filterClause,
		safeSessionSortCol(q.SortBy),
		safeSortDir(q.SortDir),
		nextIdx, nextIdx+1,
	)

	offset := q.PageSize * q.Page
	args := append([]interface{}{id}, filterArgs...)
	args = append(args, q.PageSize, offset)

	rows, err := d.db.QueryContext(ctx, query, args...)
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

// GetSessionCount returns the number of concluded sessions for a steamid,
// respecting the same filter as GetSessions.
func (d *DB) GetSessionCount(ctx context.Context, id SteamID, f SessionFilter) (int64, error) {
	filterClause, filterArgs, _ := sessionWhereArgs(f, 2)

	query := fmt.Sprintf("SELECT COUNT(*) FROM sessions WHERE steamid = $1%s", filterClause)
	args := append([]interface{}{id}, filterArgs...)

	var count int64
	err := d.db.QueryRowContext(ctx, query, args...).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

// AddSession
//
// Add a concluded session to the database
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

// GetGameCache
//
// returns game information from cache
func (d *DB) GetGameCache(ctx context.Context, appid AppID) (*GameCache, error) {
	var game GameCache
	err := d.db.QueryRowContext(ctx, "SELECT appid, name, publisher, developer, header_image, recommendations FROM games WHERE appid = $1", appid).Scan(&game.AppID, &game.Name, &game.Publisher, &game.Developer, &game.HeaderImage, &game.Recommendations)
	if err != nil {
		return nil, err
	}
	return &game, nil
}

// AddGameCache
//
// Adds game information to cache
func (d *DB) AddGameCache(ctx context.Context, game GameCache) error {
	stmt, err := d.db.PrepareContext(ctx, "INSERT INTO games(appid, name, publisher, developer, header_image, recommendations) VALUES($1, $2, $3, $4, $5, $6)")
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

// --- Users (Admin) ---

type User struct {
	SteamID  SteamID
	Username string
	Active   bool
	Public   bool
}

// ErrDuplicateSteamID is returned when inserting a user that already exists.
var ErrDuplicateSteamID = errors.New("duplicate steamid")

// ErrUserNotFound is returned when a user operation targets a non-existent row.
var ErrUserNotFound = errors.New("user not found")

// GetUsers returns a paginated list of all users and the total count.
func (d *DB) GetUsers(ctx context.Context, limit, offset int) ([]User, int64, error) {
	var total int64
	if err := d.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := d.db.QueryContext(ctx,
		"SELECT steamid, username, active, public FROM users ORDER BY steamid LIMIT $1 OFFSET $2",
		limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.SteamID, &u.Username, &u.Active, &u.Public); err != nil {
			return nil, 0, err
		}
		users = append(users, u)
	}
	return users, total, rows.Err()
}

// AddUser inserts a new user row.
func (d *DB) AddUser(ctx context.Context, id SteamID, username string, active, public bool) error {
	_, err := d.db.ExecContext(ctx,
		"INSERT INTO users(steamid, username, active, public) VALUES($1, $2, $3, $4)",
		id, username, active, public)
	if err != nil {
		if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "23505" {
			return ErrDuplicateSteamID
		}
		return wrapErr(err)
	}
	return nil
}

// RemoveUser deletes a user row by steamid.
func (d *DB) RemoveUser(ctx context.Context, id SteamID) error {
	res, err := d.db.ExecContext(ctx, "DELETE FROM users WHERE steamid = $1", id)
	if err != nil {
		return wrapErr(err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrUserNotFound
	}
	return nil
}

// ModifyUserParams holds the optional fields for ModifyUser.
// A nil pointer means "do not update this field".
type ModifyUserParams struct {
	Username *string
	Active   *bool
	Public   *bool
}

// ModifyUser updates only the non-nil fields of a user row.
func (d *DB) ModifyUser(ctx context.Context, id SteamID, p ModifyUserParams) error {
	var setClauses []string
	var args []interface{}
	i := 1

	if p.Username != nil {
		setClauses = append(setClauses, fmt.Sprintf("username = $%d", i))
		args = append(args, *p.Username)
		i++
	}
	if p.Active != nil {
		setClauses = append(setClauses, fmt.Sprintf("active = $%d", i))
		args = append(args, *p.Active)
		i++
	}
	if p.Public != nil {
		setClauses = append(setClauses, fmt.Sprintf("public = $%d", i))
		args = append(args, *p.Public)
		i++
	}

	if len(setClauses) == 0 {
		return nil // nothing to update
	}

	args = append(args, id)
	query := fmt.Sprintf("UPDATE users SET %s WHERE steamid = $%d",
		strings.Join(setClauses, ", "), i)

	res, err := d.db.ExecContext(ctx, query, args...)
	if err != nil {
		return wrapErr(err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrUserNotFound
	}
	return nil
}

// GetActiveSteamIDs returns steamids for users where active = true.
func (d *DB) GetActiveSteamIDs(ctx context.Context) ([]SteamID, error) {
	rows, err := d.db.QueryContext(ctx, "SELECT steamid FROM users WHERE active = true")
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	var ids []SteamID
	for rows.Next() {
		var id SteamID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// --- Auth Tokens ---

// ErrDuplicateTokenName is returned when a token with that name already exists.
var ErrDuplicateTokenName = errors.New("duplicate token name")

// AuthToken is the full row from auth_tokens.
type AuthToken struct {
	ID         int
	Name       string
	Salt       string
	Secret     string
	Clearance  int
	CreateDate time.Time
}

// AuthTokenInfo is the safe public projection (no salt/secret).
type AuthTokenInfo struct {
	Name       string
	Clearance  int
	CreateDate time.Time
}

// GetAuthToken fetches a single auth_tokens row by name.
func (d *DB) GetAuthToken(name string) (AuthToken, error) {
	var t AuthToken
	err := d.db.QueryRow(
		"SELECT id, name, salt, secret, clearance, create_date FROM auth_tokens WHERE name = $1", name,
	).Scan(&t.ID, &t.Name, &t.Salt, &t.Secret, &t.Clearance, &t.CreateDate)
	return t, err
}

// ListAuthTokensBelowClearance returns token info for rows with clearance < limit.
func (d *DB) ListAuthTokensBelowClearance(ctx context.Context, clearanceLimit int) ([]AuthTokenInfo, error) {
	rows, err := d.db.QueryContext(ctx,
		"SELECT name, clearance, create_date FROM auth_tokens WHERE clearance < $1 ORDER BY create_date DESC",
		clearanceLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tokens []AuthTokenInfo
	for rows.Next() {
		var t AuthTokenInfo
		if err := rows.Scan(&t.Name, &t.Clearance, &t.CreateDate); err != nil {
			return nil, err
		}
		tokens = append(tokens, t)
	}
	return tokens, rows.Err()
}

// CreateAuthToken inserts a new auth token row.
func (d *DB) CreateAuthToken(ctx context.Context, name, salt, secret string, clearance int) error {
	_, err := d.db.ExecContext(ctx,
		"INSERT INTO auth_tokens(name, salt, secret, clearance) VALUES($1, $2, $3, $4)",
		name, salt, secret, clearance)
	if err != nil {
		if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "23505" {
			return ErrDuplicateTokenName
		}
		return wrapErr(err)
	}
	return nil
}

// DeleteAuthToken removes an auth token by name.
func (d *DB) DeleteAuthToken(ctx context.Context, name string) error {
	_, err := d.db.ExecContext(ctx, "DELETE FROM auth_tokens WHERE name = $1", name)
	return wrapErr(err)
}

// --- Metadata ---

const MetaKeyLastUserReload = "last_user_reload"

// GetMetadata fetches the data value for a metadata key.
func (d *DB) GetMetadata(ctx context.Context, key string) (string, error) {
	var data string
	err := d.db.QueryRowContext(ctx, "SELECT data FROM metadata WHERE key = $1", key).Scan(&data)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return data, err
}

// SetMetadata upserts a metadata key-value pair.
func (d *DB) SetMetadata(ctx context.Context, key, data string) error {
	_, err := d.db.ExecContext(ctx,
		"INSERT INTO metadata(key, data) VALUES($1, $2) ON CONFLICT (key) DO UPDATE SET data = $2",
		key, data)
	return wrapErr(err)
}
