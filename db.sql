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

-- Games (Cached)
-- Cache game information mentioned in sessions
-- Updates every hour
CREATE TABLE IF NOT EXISTS games (
    appid integer,
    name text NOT NULL,
    publisher text,
    developer text,
    header_image text,
    recommendations integer,
    PRIMARY KEY (appid)
);

-- Registered Users
CREATE TABLE IF NOT EXISTS users (
	steamid bigint,
	username text UNIQUE NOT NULL, -- Internal username
	alias text, -- Steam display name
	profileurl text, -- URL to Steam profile
	avatar text, -- URL to avatar image
	timezone text, -- TODO: Subject to change
	active boolean NOT NULL,
	public boolean NOT NULL,
	PRIMARY KEY (steamid)
);
