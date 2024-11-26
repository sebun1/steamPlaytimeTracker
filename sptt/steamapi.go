package sptt

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
)

type SteamAPI struct {
	APIKey string
}

func NewSteamAPI(apiKey string) *SteamAPI {
	return &SteamAPI{APIKey: apiKey}
}

func (s *SteamAPI) GetPlayerSummaries(steamids string) interface{} {
	url := "http://api.steampowered.com/ISteamUser/GetPlayerSummaries/v0002/?key=" + s.APIKey + "&steamids=" + steamids
	var resp interface{}
	err := s.getJSONResp(url, &resp)
	check(err)
	return resp
}

func (s *SteamAPI) getJSONResp(url string, target interface{}) error {
	resp, err := http.Get(url)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()

	fmt.Printf("Request URI: %s\n", url)
	fmt.Printf("%v\n", resp)

	return json.NewDecoder(resp.Body).Decode(target)
}
