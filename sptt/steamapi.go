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

const (
	ErrForbidden   = APIError("API responded with 403 Forbidden")
	ErrEmptyGames  = APIError("API returned 0 games")
	RequestTimeout = 13
)

type APIError string

func (e APIError) Error() string {
	return string(e)
}

type SteamAPI struct {
	apiKey string
	client *http.Client
}

type PlayerSummary struct {
	Steamid       string `json:"steamid"`
	Visibility    int    `json:"communityvisibilitystate"` // 1: private, 3: public
	Profilestate  int    `json:"profilestate"`
	Personaname   string `json:"personaname"`
	Profileurl    string `json:"profileurl"`
	Avatar        string `json:"avatarfull"`
	Country       string `json:"loccountrycode"`
	Gameid        string `json:"gameid"`
	Gameextrainfo string `json:"gameextrainfo"`
}

type PlayerSummaryResponse struct {
	Response struct {
		Players []PlayerSummary `json:"players"`
	} `json:"response"`
}

type OwnedGame struct {
	Appid           int    `json:"appid"`
	Name            string `json:"name"`
	Playtime        int    `json:"playtime_forever"`      // in minutes
	RTimeLastPlayed int    `json:"rtime_last_played"`     // Unix timestamp
	PlaytimeDc      int    `json:"playtime_disconnected"` // in minutes
}

type OwnedGamesResponse struct {
	Response struct {
		GameCount int         `json:"game_count"`
		Games     []OwnedGame `json:"games"`
	} `json:"response"`
}

type GameData struct {
	Name             string   `json:"name"`
	SteamAppID       uint32   `json:"steam_appid"`
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

func NewSteamAPI(apiKey string) *SteamAPI {
	return &SteamAPI{
		apiKey: apiKey,
		client: &http.Client{},
	}

}

func (s *SteamAPI) TestAPIKey(ctx context.Context) (err error) {
	_, err = s.GetPlayerSummaries(ctx, []string{"76561198000000000"})
	return err
}

// TODO: Steam only allows 100 steamids per request, need to handle this if more than 100
func (s *SteamAPI) GetPlayerSummaries(ctx context.Context, steamids []string) (summaries map[string]PlayerSummary, err error) {
	if len(steamids) == 0 {
		return nil, fmt.Errorf("steamids cannot be empty")
	}

	var steamidsStr string = steamids[0]
	for i := 1; i < len(steamids); i++ {
		steamidsStr += "," + steamids[i]
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

	summaries = make(map[string]PlayerSummary)
	for _, summary := range allSummaries {
		summaries[summary.Steamid] = summary
	}
	return
}

// TODO: This API is rate limited, only 200 requests per 5 minutes
// if we want to update more than 200 games, we need to wait..
func (s *SteamAPI) GetGameDetails(ctx context.Context, appid string) (resp interface{}, err error) {
	url := "https://store.steampowered.com/api/appdetails?appids=" + appid
	resp, err = s.getRespBody(ctx, url)
	return
}

func (s *SteamAPI) GetOwnedGames(ctx context.Context, steamid string, appids []string) (games map[string]OwnedGame, err error) {
	if len(steamid) != 17 {
		return nil, fmt.Errorf("steamid should be 17 characters long")
	}
	if len(appids) == 0 {
		return nil, fmt.Errorf("appids cannot be empty")
	}

	type inputJSON struct {
		Steamid                uint64   `json:"steamid"`
		IncludeAppInfo         bool     `json:"include_appinfo"`
		IncludePlayedFreeGames bool     `json:"include_played_free_games"`
		Appids                 []uint32 `json:"appids_filter"`
	}

	steamidUint, err := strconv.ParseUint(steamid, 10, 64)
	if err != nil {
		return nil, wrapErr(err)
	}

	appidsUint := make([]uint32, len(appids))
	for i, appid := range appids {
		appidUint, err := strconv.ParseUint(appid, 10, 32)
		if err != nil {
			return nil, wrapErr(err)
		}
		appidsUint[i] = uint32(appidUint)
	}

	jsonInputPrim := inputJSON{
		Steamid:                steamidUint,
		IncludeAppInfo:         true,
		IncludePlayedFreeGames: true,
		Appids:                 appidsUint,
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
	games = make(map[string]OwnedGame)
	for _, game := range allGames {
		games[fmt.Sprint(game.Appid)] = game
	}
	return
}

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
