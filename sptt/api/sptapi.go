package api

import (
	"context"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sebun1/steamPlaytimeTracker/log"
	"github.com/sebun1/steamPlaytimeTracker/sptt"
)

const (
	defaultPageSize int32 = 20
	maxPageSize     int32 = 100
)

type SpttAPI struct {
	ctx       context.Context
	db        *sptt.DB
	notifChan chan sptt.Notif
	wg        *sync.WaitGroup
	addr      string
}

func NewSpttAPI(ctx context.Context, db *sptt.DB, notifChan chan sptt.Notif, wg *sync.WaitGroup, addr string) *SpttAPI {
	return &SpttAPI{
		ctx:       ctx,
		db:        db,
		notifChan: notifChan,
		wg:        wg,
		addr:      addr,
	}
}

func (a *SpttAPI) Run() {
	defer a.wg.Done()

	r := gin.Default()

	r.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "pong"})
	})

	users := r.Group("/users/:id")
	{
		users.GET("/sessions", a.getSessions)
		users.GET("/active_sessions", a.getActiveSessions)
		users.GET("/stats", a.getUserStats)
	}

	srv := &http.Server{
		Addr:    a.addr,
		Handler: r,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Errorf("API server error: %v", err)
		}
	}()

	<-a.ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Errorf("API server shutdown error: %v", err)
	}
}

// parseSteamID extracts and validates the :id path param as a SteamID.
func parseSteamID(c *gin.Context) (sptt.SteamID, bool) {
	raw := c.Param("id")
	v, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid steam id"})
		return 0, false
	}
	return sptt.SteamID(v), true
}

// parsePage parses ?page= (0-based) and ?page_size= query params.
func parsePage(c *gin.Context) (page int32, pageSize int32) {
	page = 0
	pageSize = defaultPageSize

	if p := c.Query("page"); p != "" {
		if v, err := strconv.ParseInt(p, 10, 32); err == nil && v >= 0 {
			page = int32(v)
		}
	}
	if ps := c.Query("page_size"); ps != "" {
		if v, err := strconv.ParseInt(ps, 10, 32); err == nil && v > 0 {
			pageSize = int32(v)
		}
	}
	if pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	return
}

type sessionResponse struct {
	SteamID         uint64 `json:"steam_id"`
	AppID           uint32 `json:"app_id"`
	UTCStart        string `json:"utc_start"`
	UTCEnd          string `json:"utc_end"`
	PlaytimeForever uint32 `json:"playtime_forever"`
}

type activeSessionResponse struct {
	SteamID         uint64 `json:"steam_id"`
	AppID           uint32 `json:"app_id"`
	UTCStart        string `json:"utc_start"`
	PlaytimeForever uint32 `json:"playtime_forever"`
}

type paginatedSessions struct {
	Data       []sessionResponse `json:"data"`
	Page       int32             `json:"page"`
	PageSize   int32             `json:"page_size"`
	TotalCount int64             `json:"total_count"`
	TotalPages int32             `json:"total_pages"`
}

// parseSessionQuery builds a SessionQuery from the request's query params.
// Sort params: sort_by (appid|utcstart|utcend|playtime_forever), sort_dir (asc|desc).
// Filter params: app_id, utcstart_from, utcstart_to, utcend_from, utcend_to,
//
//	playtime_min, playtime_max (times in RFC3339, e.g. 2024-01-01T00:00:00Z).
func parseSessionQuery(c *gin.Context) sptt.SessionQuery {
	page, pageSize := parsePage(c)

	sortBy := sptt.SortByUTCStart
	switch c.Query("sort_by") {
	case "appid":
		sortBy = sptt.SortByAppID
	case "utcend":
		sortBy = sptt.SortByUTCEnd
	case "playtime_forever":
		sortBy = sptt.SortByPlaytimeForever
	}

	sortDir := sptt.SortDirAsc
	if c.Query("sort_dir") == "desc" {
		sortDir = sptt.SortDirDesc
	}

	var filter sptt.SessionFilter

	if v := c.Query("app_id"); v != "" {
		if n, err := strconv.ParseUint(v, 10, 32); err == nil {
			appid := sptt.AppID(n)
			filter.AppID = &appid
		}
	}
	if v := c.Query("utcstart_from"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			filter.UTCStartFrom = &t
		}
	}
	if v := c.Query("utcstart_to"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			filter.UTCStartTo = &t
		}
	}
	if v := c.Query("utcend_from"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			filter.UTCEndFrom = &t
		}
	}
	if v := c.Query("utcend_to"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			filter.UTCEndTo = &t
		}
	}
	if v := c.Query("playtime_min"); v != "" {
		if n, err := strconv.ParseUint(v, 10, 32); err == nil {
			u := uint32(n)
			filter.PlaytimeForeverMin = &u
		}
	}
	if v := c.Query("playtime_max"); v != "" {
		if n, err := strconv.ParseUint(v, 10, 32); err == nil {
			u := uint32(n)
			filter.PlaytimeForeverMax = &u
		}
	}

	return sptt.SessionQuery{
		Page:     page,
		PageSize: pageSize,
		SortBy:   sortBy,
		SortDir:  sortDir,
		Filter:   filter,
	}
}

// GET /users/:id/sessions
//
// Query params: page, page_size, sort_by, sort_dir, app_id,
// utcstart_from, utcstart_to, utcend_from, utcend_to, playtime_min, playtime_max
func (a *SpttAPI) getSessions(c *gin.Context) {
	id, ok := parseSteamID(c)
	if !ok {
		return
	}

	q := parseSessionQuery(c)

	totalCount, err := a.db.GetSessionCount(a.ctx, id, q.Filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to count sessions"})
		return
	}

	sessions, err := a.db.GetSessions(a.ctx, id, q)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get sessions"})
		return
	}

	data := make([]sessionResponse, 0, len(sessions))
	for _, s := range sessions {
		data = append(data, sessionResponse{
			SteamID:         uint64(s.SteamID),
			AppID:           uint32(s.AppID),
			UTCStart:        s.UTCStart.Format("2006-01-02T15:04:05Z"),
			UTCEnd:          s.UTCEnd.Format("2006-01-02T15:04:05Z"),
			PlaytimeForever: s.PlaytimeForever,
		})
	}

	totalPages := int32((totalCount + int64(q.PageSize) - 1) / int64(q.PageSize))

	c.JSON(http.StatusOK, paginatedSessions{
		Data:       data,
		Page:       q.Page,
		PageSize:   q.PageSize,
		TotalCount: totalCount,
		TotalPages: totalPages,
	})
}

// GET /users/:id/active_sessions
func (a *SpttAPI) getActiveSessions(c *gin.Context) {
	id, ok := parseSteamID(c)
	if !ok {
		return
	}

	sessionsMap, err := a.db.GetActiveSessions(a.ctx, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get active sessions"})
		return
	}

	data := make([]activeSessionResponse, 0, len(sessionsMap))
	for _, s := range sessionsMap {
		data = append(data, activeSessionResponse{
			SteamID:         uint64(s.SteamID),
			AppID:           uint32(s.AppID),
			UTCStart:        s.UTCStart.Format("2006-01-02T15:04:05Z"),
			PlaytimeForever: s.PlaytimeForever,
		})
	}

	c.JSON(http.StatusOK, gin.H{"data": data})
}

type userStatsResponse struct {
	SteamID       uint64 `json:"steam_id"`
	TotalSessions int64  `json:"total_sessions"`
}

// GET /users/:id/stats
func (a *SpttAPI) getUserStats(c *gin.Context) {
	id, ok := parseSteamID(c)
	if !ok {
		return
	}

	totalSessions, err := a.db.GetSessionCount(a.ctx, id, sptt.SessionFilter{})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get stats"})
		return
	}

	c.JSON(http.StatusOK, userStatsResponse{
		SteamID:       uint64(id),
		TotalSessions: totalSessions,
	})
}
