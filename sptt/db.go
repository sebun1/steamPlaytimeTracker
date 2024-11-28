package sptt

import (
	"context"
	"database/sql"
	"fmt"
	"os"

	"github.com/sebun1/steamPlaytimeTracker/log"

	_ "github.com/lib/pq"
)

type DB struct {
	db *sql.DB
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
	err = thisdb.init()
	if err != nil {
		return nil, err
	}

	return thisdb, nil
}

func (d *DB) Close() {
	d.db.Close()
}

func (d *DB) init() error {
	var query []byte
	query, err := os.ReadFile("db.sql")
	if err != nil {
		return err
	}

	log.Debug("Create DB Query:\n", string(query))

	_, err = d.db.Exec(string(query))
	if err != nil {
		return err
	}

	return nil
}

func (d *DB) GetSteamIDs(ctx context.Context) ([]string, error) {
	rows, err := d.db.QueryContext(ctx, "SELECT steamid FROM users")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	steamids := []string{}
	for rows.Next() {
		var steamid string
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

func (d *DB) AddSteamID(ctx context.Context, steamid []string) error {
	stmt, err := d.db.PrepareContext(ctx, "INSERT INTO users(steamid, enabled, public) VALUES(?, true, true)")
	if err != nil {
		return wrapErr(err)
	}
	defer stmt.Close()

	for _, id := range steamid {
		if _, err = stmt.Exec(id); err != nil {
			return wrapErr(err)
		}
	}
	return nil
}
