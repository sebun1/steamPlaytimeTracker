package sptt

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/sebun1/steamPlaytimeTracker/log"
)

const (
	ErrForbidden   = APIError("API responded with 403 Forbidden")
	RequestTimeout = 13
)

type APIError string

func (e APIError) Error() string {
	return string(e)
}

type SteamAPI struct {
	APIKey string
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

func NewSteamAPI(apiKey string) *SteamAPI {
	api := SteamAPI{APIKey: apiKey}
	api.client = &http.Client{}
	return &api
}

func (s *SteamAPI) TestAPIKey(ctx context.Context) (err error) {
	_, err = s.GetPlayerSummaries(ctx, []string{"76561198000000000"})
	return err
}

// TODO: Steam only allows 100 steamids per request, need to handle this
func (s *SteamAPI) GetPlayerSummaries(ctx context.Context, steamids []string) (summaries map[string]PlayerSummary, err error) {
	var steamidsStr string = steamids[0]
	for i := 1; i < len(steamids); i++ {
		steamidsStr += "," + steamids[i]
	}
	url := "https://api.steampowered.com/ISteamUser/GetPlayerSummaries/v0002/?key=" + s.APIKey + "&steamids=" + steamidsStr

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
	summaries = make(map[string]PlayerSummary)
	for _, summary := range allSummaries {
		summaries[summary.Steamid] = summary
	}
	return
}

// TODO: Filters can be applied to the request, otherwise cannot request multiple
// Add implementation for multiple appid at once
func (s *SteamAPI) GetGameDetails(ctx context.Context, appid string) (resp interface{}, err error) {
	url := "https://store.steampowered.com/api/appdetails?appids=" + appid
	resp, err = s.getRespBody(ctx, url)
	return
}

func (s *SteamAPI) getRespBody(ctx context.Context, url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, RequestTimeout*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("Error while creating http request: %w", err)
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
		log.Error("Request URI: ", strings.ReplaceAll(url, s.APIKey, "<API_KEY_REDACTED>"))
		log.Error("Response: ", bodyStr)
		if resp.StatusCode == 403 {
			return nil, ErrForbidden
		}
		return nil, fmt.Errorf("Error Response http status code %d", resp.StatusCode)
	}

	log.Debug("Response Body:\n", bodyStr)

	return body, nil
}
