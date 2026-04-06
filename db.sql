-- DROP TABLE FOR DEV ENVIRONMENT
-- DROP TABLE IF EXISTS active_sessions;
-- DROP TABLE IF EXISTS sessions;
-- DROP TABLE IF EXISTS games;
-- DROP TABLE IF EXISTS users;



-- Active Sessions (Temporary Data)
CREATE TABLE IF NOT EXISTS active_sessions (
    steamid bigint,
    appid integer,
    utcstart timestamp,
	playtime_forever integer, -- Total playtime in minutes according to Steam API
    PRIMARY KEY (steamid, appid)
);

-- Sessions (History/Permanent Data)
CREATE TABLE IF NOT EXISTS sessions (
    steamid bigint,
    utcstart timestamp,
    utcend timestamp,
	playtime_forever integer, -- Total playtime in minutes according to Steam API
    appid integer,
    PRIMARY KEY (steamid, utcstart)
);

CREATE INDEX IF NOT EXISTS idx_sessions_steamid ON sessions(steamid);

-- Auth Tokens
CREATE TABLE IF NOT EXISTS auth_tokens (
    id          SERIAL PRIMARY KEY,
    name        VARCHAR(64)  NOT NULL UNIQUE,
    salt        VARCHAR(64)  NOT NULL,
    secret      VARCHAR(128) NOT NULL,
    clearance   INT          NOT NULL,
    create_date TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- Metadata (key-value store for server state)
CREATE TABLE IF NOT EXISTS metadata (
    id   SERIAL PRIMARY KEY,
    key  TEXT NOT NULL UNIQUE,
    data TEXT NOT NULL
);

-- Registered Users
CREATE TABLE IF NOT EXISTS users (
	steamid bigint,
	username text UNIQUE NOT NULL, -- Internal username
	active boolean NOT NULL,
	public boolean NOT NULL,
	PRIMARY KEY (steamid)
);
