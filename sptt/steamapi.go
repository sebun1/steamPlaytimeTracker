package sptt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/sebun1/steamPlaytimeTracker/log"
)

type SteamAPI struct {
	apiKey string
	client *http.Client
}

// Errors associated with Steam API operations
const (
	ErrForbidden   = APIError("API responded with 403 Forbidden")
	ErrEmptyGames  = APIError("API returned 0 games")
	RequestTimeout = 13
)

type APIError string

func (e APIError) Error() string {
	return string(e)
}

// Data Types for certain steam attributes
type SteamID uint64

func (s *SteamID) String() string {
	return strconv.FormatUint(uint64(*s), 10)
}

func (s *SteamID) UnmarshalJSON(b []byte) error {
	var idStr string
	var id uint64
	if err := json.Unmarshal(b, &idStr); err != nil {
		err = json.Unmarshal(b, &id)
		if err != nil {
			return err
		}
	} else {
		id, err = strconv.ParseUint(idStr, 10, 64)
		if err != nil {
			return err
		}
	}

	*s = SteamID(id)
	return nil
}

type AppID uint32

func (a *AppID) String() string {
	return strconv.FormatUint(uint64(*a), 10)
}

func (a *AppID) UnmarshalJSON(b []byte) error {
	var idStr string
	var id uint64
	if err := json.Unmarshal(b, &idStr); err != nil {
		err = json.Unmarshal(b, &id)
		if err != nil {
			return err
		}
	} else {
		id, err = strconv.ParseUint(idStr, 10, 32)
		if err != nil {
			return err
		}
	}

	*a = AppID(id)
	return nil
}

// Data Types for Steam API responses

type GameData struct {
	Name             string   `json:"name"`
	AppID            AppID    `json:"steam_appid"`
	ShortDescription string   `json:"short_description"`
	HeaderImage      string   `json:"header_image"`
	Developers       []string `json:"developers"`
	Publishers       []string `json:"publishers"`
	Platforms        struct {
		Windows bool `json:"windows"`
		Mac     bool `json:"mac"`
		Linux   bool `json:"linux"`
	}
	ReleaseDate struct {
		ComingSoon bool   `json:"coming_soon"`
		Date       string `json:"date"`
	}
	Background string `json:"background_raw"`
}

type GameDataResponse struct {
	Games map[string]struct {
		Success bool     `json:"success"`
		Data    GameData `json:"data"`
	}
}

// NewSteamAPI creates a new SteamAPI object
// SteamAPI itself should never be declared directly
func NewSteamAPI(apiKey string) *SteamAPI {
	return &SteamAPI{
		apiKey: apiKey,
		client: &http.Client{},
	}

}

func (s *SteamAPI) TestAPIKey(ctx context.Context) (err error) {
	_, err = s.GetPlayerSummaries(ctx, []SteamID{76561198000000000})
	return err
}

type PlayerSummary struct {
	SteamID       SteamID `json:"steamid"`
	Visibility    int     `json:"communityvisibilitystate"` // 1: private, 3: public
	Profilestate  int     `json:"profilestate"`
	Personaname   string  `json:"personaname"`
	Profileurl    string  `json:"profileurl"`
	Avatar        string  `json:"avatarfull"`
	Country       string  `json:"loccountrycode"`
	GameID        AppID   `json:"gameid"`
	Gameextrainfo string  `json:"gameextrainfo"`
}

type PlayerSummaryResponse struct {
	Response struct {
		Players []PlayerSummary `json:"players"`
	} `json:"response"`
}

func (s *SteamAPI) GetPlayerSummary(ctx context.Context, steamid SteamID) (summary PlayerSummary, err error) {
	summaries, err := s.GetPlayerSummaries(ctx, []SteamID{steamid})
	if err != nil {
		return
	}

	summary, ok := summaries[steamid]
	if !ok {
		return summary, fmt.Errorf("steamid %d not found in response", steamid)
	}
	return
}

// TODO: Steam only allows 100 steamids per request, need to handle this if more than 100
func (s *SteamAPI) GetPlayerSummaries(ctx context.Context, steamids []SteamID) (summaries map[SteamID]PlayerSummary, err error) {
	if len(steamids) == 0 {
		return nil, fmt.Errorf("steamids cannot be empty")
	}

	var steamidsStr string = steamids[0].String()
	for i := 1; i < len(steamids); i++ {
		steamidsStr += "," + steamids[i].String()
	}
	url := "https://api.steampowered.com/ISteamUser/GetPlayerSummaries/v2/?key=" + s.apiKey + "&steamids=" + steamidsStr

	body, err := s.getRespBody(ctx, url)
	if err != nil {
		return nil, err
	}

	var resp PlayerSummaryResponse
	err = json.Unmarshal(body, &resp)
	if err != nil {
		return nil, wrapErr(err)
	}

	allSummaries := resp.Response.Players

	if len(allSummaries) != len(steamids) {
		return nil, fmt.Errorf("steam api returned %d summaries, expected %d", len(allSummaries), len(steamids))
	}

	summaries = make(map[SteamID]PlayerSummary)
	for _, summary := range allSummaries {
		summaries[summary.SteamID] = summary
	}
	return
}

// TODO: This API is rate limited, only 200 requests per 5 minutes
// if we want to update more than 200 games, we need to wait..
func (s *SteamAPI) GetGameDetails(ctx context.Context, appid AppID) (resp interface{}, err error) {
	url := "https://store.steampowered.com/api/appdetails?appids=" + appid.String()
	resp, err = s.getRespBody(ctx, url)
	return
}

type OwnedGame struct {
	AppID           AppID  `json:"appid"`
	Name            string `json:"name"`
	Playtime        uint32 `json:"playtime_forever"`      // in minutes
	RTimeLastPlayed uint32 `json:"rtime_last_played"`     // Unix timestamp
	PlaytimeDc      uint32 `json:"playtime_disconnected"` // in minutes
}

type OwnedGamesResponse struct {
	Response struct {
		GameCount uint        `json:"game_count"`
		Games     []OwnedGame `json:"games"`
	} `json:"response"`
}

func (s *SteamAPI) GetOwnedGame(ctx context.Context, steamid SteamID, appid AppID) (OwnedGame, error) {
	game := OwnedGame{}
	games, err := s.GetOwnedGames(ctx, steamid, []AppID{appid})
	if err != nil {
		return game, err
	}

	game, ok := games[appid]
	if !ok {
		return game, fmt.Errorf("appid %d not found in response", appid)
	}
	return game, nil
}

func (s *SteamAPI) GetOwnedGames(ctx context.Context, steamid SteamID, appids []AppID) (games map[AppID]OwnedGame, err error) {
	type inputJSON struct {
		Steamid                uint64   `json:"steamid"`
		IncludeAppInfo         bool     `json:"include_appinfo"`
		IncludePlayedFreeGames bool     `json:"include_played_free_games"`
		Appids                 []uint32 `json:"appids_filter"`
	}

	jsonInputPrim := inputJSON{
		Steamid:                uint64(steamid),
		IncludeAppInfo:         true,
		IncludePlayedFreeGames: true,
		Appids:                 make([]uint32, len(appids)),
	}

	for i, appid := range appids {
		jsonInputPrim.Appids[i] = uint32(appid)
	}

	jsonStrBytes, err := json.Marshal(jsonInputPrim)
	if err != nil {
		return nil, wrapErr(err)
	}

	newBuf := bytes.NewBuffer(nil)
	if err := json.Compact(newBuf, jsonStrBytes); err != nil {
		return nil, wrapErr(err)
	}

	url := "https://api.steampowered.com/IPlayerService/GetOwnedGames/v1/?key=" + s.apiKey + "&format=json&input_json=" + url.QueryEscape(newBuf.String())

	body, err := s.getRespBody(ctx, url)
	if err != nil {
		return nil, err
	}

	var resp OwnedGamesResponse
	err = json.Unmarshal(body, &resp)
	if err != nil {
		return nil, wrapErr(err)
	}

	if resp.Response.GameCount == 0 || len(resp.Response.Games) == 0 {
		log.Error("Steam API returned 0 games, this function should only be called with user owned game appids, returning error")
		return nil, ErrEmptyGames
	}

	allGames := resp.Response.Games
	games = make(map[AppID]OwnedGame)
	for _, game := range allGames {
		games[game.AppID] = game
	}
	return
}

// getRespBody sends a GET request to the given URL and returns the response body
// It also checks the response status code and ensures it is 200
func (s *SteamAPI) getRespBody(ctx context.Context, url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, RequestTimeout*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("Error while creating http request: %w", err)
	}

	if s.client == nil {
		log.Warn("HTTP client is nil when it shouldn't, creating new one")
		s.client = &http.Client{}
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Error while sending http request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("Error while reading response body of http request: %w", err)
	}

	bodyStr := string(body)

	if resp.StatusCode != http.StatusOK {
		log.Error("HTTP request failed with status code ", resp.StatusCode)
		log.Error("Request URI: ", strings.ReplaceAll(url, s.apiKey, "<API_KEY_REDACTED>"))
		log.Error("Response: ", bodyStr)
		if resp.StatusCode == 403 {
			return nil, ErrForbidden
		}
		return nil, fmt.Errorf("Error Response http status code %d", resp.StatusCode)
	}

	log.Debug("Response Body:\n", bodyStr)

	return body, nil
}
