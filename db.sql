-- DROP TABLE FOR DEV ENVIRONMENT
DROP TABLE IF EXISTS active_sessions;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS games;
DROP TABLE IF EXISTS users;



-- Active Sessions (Temporary Data)
CREATE TABLE IF NOT EXISTS active_sessions (
    steamid char(17),
    utcstart timestamp,
	playtime_forever integer, -- Total playtime in minutes according to Steam API
    appid integer,
    PRIMARY KEY (steamid, utcstart)
);

-- Sessions (History/Permanent Data)
CREATE TABLE IF NOT EXISTS sessions (
    steamid varchar(17),
    utcstart timestamp,
    utcend timestamp,
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
    recommendations integer NOT NULL,
    PRIMARY KEY (appid)
);

-- Registered Users
CREATE TABLE IF NOT EXISTS users (
	steamid varchar(17),
	name text,
	profileurl text,
	avatar text,
	timezone text, -- TODO: Subject to change
	enabled boolean NOT NULL,
	public boolean NOT NULL,
	PRIMARY KEY (steamid)
);

INSERT INTO users (steamid, enabled, public) VALUES ('76561198854733565', true, true); -- Takina
INSERT INTO users (steamid, enabled, public) VALUES ('76561199079920297', true, true); -- 7
